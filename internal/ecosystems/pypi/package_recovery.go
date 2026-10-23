package pypi

import (
	"context"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/transfer"
)

func (s *Service) recoverFetchedPackage(ctx context.Context, req Request, route Route, cfg config.UpstreamConfig, fetched *upstreamFetch) *upstreamFetch {
	if methodOrGet(req.Method) != http.MethodGet || (fetched.status != http.StatusOK && fetched.status != http.StatusPartialContent) {
		return fetched
	}
	// Each attempt owns its transport context. Completing or closing an attempt
	// must not cancel the outer packageBody while a verified range is reopened.
	transferCtx, cancel := context.WithCancel(ctx)
	expectedSHA256 := packageRecoveryDigest(req, route, fetched)
	requestURL := fetched.requestURL
	body := transfer.HTTPRecovery(transferCtx, methodOrGet(req.Method), fetched.headers, fetched.body, expectedSHA256,
		func(retryCtx context.Context, headers http.Header) (*transfer.Response, error) {
			retryReq := req
			retryReq.Range, retryReq.IfRange = headers.Get("Range"), headers.Get("If-Range")
			attempt, err := s.fetchPackageURL(retryCtx, retryReq, cfg, requestURL)
			if err != nil {
				return nil, err
			}
			attempt, err = selectFetchedPackageRange(retryReq, attempt)
			if err != nil {
				return nil, err
			}
			return &transfer.Response{
				Status: attempt.status, Headers: attempt.headers, Body: attempt.body, Size: contentLength(attempt.headers),
			}, nil
		})
	fetched.body = transfer.CancelOnClose(body, cancel)
	fetched.transferContext = transferCtx
	return fetched
}

func packageRecoveryDigest(req Request, route Route, fetched *upstreamFetch) string {
	// A selected range can close before the full digest is verified, so its
	// continuation must be identified by a strong ETag instead.
	if packageIsMetadata(route) || (req.Range != "" && transfer.MatchesIfRange(req.IfRange, fetched.headers)) {
		return ""
	}
	return fetched.expectedSHA256
}
