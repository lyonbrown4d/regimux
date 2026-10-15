package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

const (
	maxForbiddenEvidenceBytes = 32 << 10
	forbiddenEvidenceTimeout  = time.Second
)

func isPublicAnonymousMirror(runtime upstreamRuntime) bool {
	return runtime.isMirror &&
		normalizeMirrorForbiddenPolicy(runtime.config.MirrorForbiddenPolicy) == mirrorForbiddenPolicyPublic &&
		!isAuthenticated(runtime.config.Auth)
}

// confirmAllowlistForbidden reads bounded evidence for GET responses. HEAD
// responses have no body, so it issues a GET to the same endpoint with the
// same endpoint-scoped credentials before treating the 403 as mirror coverage.
func (c *Client) confirmAllowlistForbidden(
	ctx context.Context,
	runtime upstreamRuntime,
	spec requestSpec,
	resp upstreamResponse,
) (bool, error) {
	evidenceCtx, cancel := context.WithTimeout(ctx, forbiddenEvidenceTimeout)
	defer cancel()

	if resp.Body != nil {
		body, readErr := readBoundedBody(evidenceCtx, resp.Body)
		if readErr != nil {
			return false, requestContextError(ctx)
		}
		if spec.method != http.MethodHead {
			return isAllowlistForbiddenBody(body), nil
		}
	}

	confirmSpec := spec
	confirmSpec.method = http.MethodGet
	confirmed, err := c.execute(evidenceCtx, runtime, confirmSpec)
	if err != nil {
		return false, requestContextError(ctx)
	}
	if confirmed.StatusCode != http.StatusForbidden {
		if closeErr := closeBody(confirmed.Body); closeErr != nil {
			return false, wrapError(closeErr, "close public mirror evidence response")
		}
		return false, nil
	}
	body, err := readBoundedBody(evidenceCtx, confirmed.Body)
	if err != nil {
		return false, requestContextError(ctx)
	}
	return isAllowlistForbiddenBody(body), nil
}

func requestContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return wrapError(err, "confirm public mirror forbidden response")
	}
	return nil
}

func readBoundedBody(ctx context.Context, body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	closeResult := make(chan error, 1)
	stopClose := context.AfterFunc(ctx, func() { closeResult <- closeBody(body) })
	data, err := upstreamhttp.ReadAllLimited(body, maxForbiddenEvidenceBytes)
	var closeErr error
	if stopClose() {
		closeErr = closeBody(body)
	} else {
		closeErr = <-closeResult
	}
	if err != nil {
		return nil, wrapError(err, "read public mirror forbidden evidence")
	}
	if closeErr != nil {
		return nil, wrapError(closeErr, "close public mirror forbidden evidence body")
	}
	return data, nil
}

func isAllowlistForbiddenBody(body []byte) bool {
	if len(body) == 0 || len(body) > maxForbiddenEvidenceBytes {
		return false
	}
	var payload struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &payload) == nil {
		for _, item := range payload.Errors {
			if strings.EqualFold(item.Code, "DENIED") && strings.Contains(strings.ToLower(item.Message), "allowlist") {
				return true
			}
		}
	}
	text := strings.ToLower(string(body))
	return strings.Contains(text, "allowlist") && strings.Contains(text, "denied")
}
