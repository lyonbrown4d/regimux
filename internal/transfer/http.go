package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	ocidigest "github.com/opencontainers/go-digest"
)

// HTTPRecovery reopens the same trusted resource after a transient body failure.
// The opener must use the original caller context and merge the supplied Range,
// If-Range and Accept-Encoding headers into a fresh request to the same URL.
func HTTPRecovery(
	ctx context.Context,
	method string,
	headers http.Header,
	body io.ReadCloser,
	expectedSHA256 string,
	open func(context.Context, http.Header) (*Response, error),
) io.ReadCloser {
	if body == nil {
		return http.NoBody
	}
	identity, ok := recoveryIdentityFrom(headers, expectedSHA256)
	if method != http.MethodGet || !ok || open == nil {
		return body
	}
	recovered := false
	reader := Recover(ctx, body, identity.length(), func(retryCtx context.Context, consumed int64) (io.ReadCloser, error) {
		next, err := identity.reopen(retryCtx, consumed, open)
		if err == nil {
			recovered = true
		}
		return next, err
	})
	if identity.start == 0 && identity.end+1 == identity.total && validSHA256(expectedSHA256) {
		return &digestBody{
			ReadCloser: reader, verifier: ocidigest.Digest(normalizedSHA256(expectedSHA256)).Verifier(), size: identity.total,
			recovered: func() bool { return recovered },
		}
	}
	return reader
}

type recoveryIdentity struct {
	start int64
	end   int64
	total int64
	etag  string
}

func recoveryIdentityFrom(headers http.Header, digest string) (recoveryIdentity, bool) {
	encoding := headers.Get("Content-Encoding")
	if encoding != "" && !strings.EqualFold(encoding, "identity") {
		return recoveryIdentity{}, false
	}
	identity := recoveryIdentity{etag: headers.Get("ETag")}
	if !strongETag(identity.etag) && !validSHA256(digest) {
		return recoveryIdentity{}, false
	}
	if contentRange := headers.Get("Content-Range"); contentRange != "" {
		if !strongETag(identity.etag) {
			return recoveryIdentity{}, false
		}
		parsed, ok := parseContentRange(contentRange)
		parsed.etag = identity.etag
		return parsed, ok
	}
	size, err := strconv.ParseInt(headers.Get("Content-Length"), 10, 64)
	if err != nil || size <= 0 {
		return recoveryIdentity{}, false
	}
	identity.total, identity.end = size, size-1
	return identity, true
}

func (r recoveryIdentity) length() int64 { return r.end - r.start + 1 }

func (r recoveryIdentity) reopen(
	ctx context.Context,
	consumed int64,
	open func(context.Context, http.Header) (*Response, error),
) (io.ReadCloser, error) {
	headers := http.Header{
		"Range":           {fmt.Sprintf("bytes=%d-%d", r.start+consumed, r.end)},
		"Accept-Encoding": {"identity"},
	}
	if strongETag(r.etag) {
		headers.Set("If-Range", r.etag)
	}
	response, err := open(ctx, headers)
	if err != nil {
		if response != nil && response.Body != nil {
			err = errors.Join(err, response.Body.Close())
		}
		return nil, fmt.Errorf("request artifact continuation: %w", err)
	}
	if response == nil || response.Body == nil {
		return nil, errors.New("artifact continuation has no body")
	}
	if err := r.validate(response, consumed); err != nil {
		return nil, errors.Join(err, response.Body.Close())
	}
	return response.Body, nil
}

func (r recoveryIdentity) validate(response *Response, consumed int64) error {
	if response.Status != http.StatusPartialContent {
		return errors.New("artifact continuation did not return 206")
	}
	actual, ok := parseContentRange(response.Headers.Get("Content-Range"))
	if !ok || actual.start != r.start+consumed || actual.end != r.end || actual.total != r.total {
		return errors.New("artifact continuation changed the byte range or total length")
	}
	if strongETag(r.etag) && response.Headers.Get("ETag") != r.etag {
		return errors.New("artifact continuation changed the ETag")
	}
	return validateContinuationLength(response.Headers, actual.length())
}

func validateContinuationLength(headers http.Header, expected int64) error {
	encoding := headers.Get("Content-Encoding")
	if encoding != "" && !strings.EqualFold(encoding, "identity") {
		return errors.New("artifact continuation changed the content encoding")
	}
	if length := headers.Get("Content-Length"); length != "" {
		size, err := strconv.ParseInt(length, 10, 64)
		if err != nil || size != expected {
			return errors.New("artifact continuation has an inconsistent Content-Length")
		}
	}
	return nil
}

func parseContentRange(value string) (recoveryIdentity, bool) {
	value, ok := strings.CutPrefix(value, "bytes ")
	if !ok {
		return recoveryIdentity{}, false
	}
	interval, totalText, ok := strings.Cut(value, "/")
	if !ok {
		return recoveryIdentity{}, false
	}
	startText, endText, ok := strings.Cut(interval, "-")
	if !ok {
		return recoveryIdentity{}, false
	}
	start, startErr := strconv.ParseInt(startText, 10, 64)
	end, endErr := strconv.ParseInt(endText, 10, 64)
	total, totalErr := strconv.ParseInt(totalText, 10, 64)
	valid := startErr == nil && endErr == nil && totalErr == nil && start >= 0 && end >= start && total > end
	return recoveryIdentity{start: start, end: end, total: total}, valid
}

func strongETag(value string) bool {
	return len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)
}

func normalizedSHA256(value string) string {
	return "sha256:" + strings.TrimPrefix(strings.ToLower(value), "sha256:")
}

func validSHA256(value string) bool {
	digest := ocidigest.Digest(normalizedSHA256(value))
	return digest.Validate() == nil && digest.Algorithm() == ocidigest.SHA256
}

type digestBody struct {
	io.ReadCloser
	verifier  ocidigest.Verifier
	size      int64
	read      int64
	recovered func() bool
	terminal  error
}

func (b *digestBody) Read(payload []byte) (int, error) {
	if b.terminal != nil {
		return 0, b.terminal
	}
	n, err := b.ReadCloser.Read(payload)
	if n > 0 {
		if _, hashErr := b.verifier.Write(payload[:n]); hashErr != nil {
			b.terminal = fmt.Errorf("hash artifact continuation: %w", hashErr)
			return 0, b.terminal
		}
		b.read += int64(n)
	}
	if b.read == b.size && b.recovered() && !b.verifier.Verified() {
		// A known-length consumer can discard an error returned with its final bytes.
		b.terminal = errors.New("artifact continuation SHA-256 mismatch")
		return 0, b.terminal
	}
	return readResult(n, err)
}
