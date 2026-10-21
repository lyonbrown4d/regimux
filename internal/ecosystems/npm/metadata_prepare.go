package npm

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/spool"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

const npmMetadataMaxBytes int64 = 64 << 20
const metadataTransformConcurrency = 2

func (s *Service) prepareFetched(req Request, requestRoute route, fetched *upstreamFetch) (*upstreamFetch, error) {
	if fetched == nil || fetched.body == nil {
		return fetched, nil
	}
	if requestRoute.Kind != routeMetadata {
		return fetched, nil
	}
	body, err := upstreamhttp.ReadAllLimited(fetched.body, npmMetadataMaxBytes)
	if err != nil {
		closeReadCloser(fetched.body, s.logger, "close oversized npm metadata body")
		return nil, wrapError(err, "read npm upstream body")
	}
	closeReadCloser(fetched.body, s.logger, "close npm upstream body")

	rewritten, changed, err := rewriteMetadata(body,
		localBase(requestPublicURL(s.publicURL, req.ProxyBaseURL), requestRoute.Alias), fetched.registryBaseURL, fetched.requestURL)
	if err != nil {
		return nil, err
	}
	headers := fetched.headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("X-Regimux-Metadata-Format", npmMetadataFormat)
	payload := body
	if changed {
		headers.Del("Content-Length")
		payload = rewritten
	}
	if int64(len(payload)) > npmMetadataMaxBytes {
		return nil, errors.New("rewritten npm metadata exceeds body limit")
	}
	materialized, err := materializeMetadataBody(payload)
	if err != nil {
		return nil, wrapError(err, "spool rewritten npm metadata")
	}
	return &upstreamFetch{
		status:     fetched.status,
		headers:    headers,
		body:       materialized,
		requestURL: fetched.requestURL,
	}, nil
}

func materializeMetadataBody(body []byte) (*spool.TempBody, error) {
	tmp, err := spool.NewTempBody("regimux-npm-metadata-*")
	if err != nil {
		return nil, fmt.Errorf("create npm metadata spool: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		return nil, errors.Join(err, tmp.Close())
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, errors.Join(err, tmp.Close())
	}
	return tmp, nil
}
