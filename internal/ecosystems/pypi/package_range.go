package pypi

import (
	"context"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/transfer"
)

func (s *Service) fetchRangedPackage(ctx context.Context, req Request, route Route, cfg config.UpstreamConfig) (*upstreamFetch, error) {
	fetched, err := s.fetchPackage(ctx, req, route, cfg)
	if err != nil {
		return nil, err
	}
	fetched = s.recoverFetchedPackage(ctx, req, route, cfg, fetched)
	return selectFetchedPackageRange(req, fetched)
}

func selectFetchedPackageRange(req Request, fetched *upstreamFetch) (*upstreamFetch, error) {
	if fetched.status != http.StatusPartialContent && (methodOrGet(req.Method) != http.MethodGet || req.Range == "") {
		return fetched, nil
	}
	selected, err := transfer.ApplyRange(methodOrGet(req.Method), req.Range, req.IfRange, transfer.Response{
		Status: fetched.status, Headers: fetched.headers, Body: fetched.body, Size: contentLength(fetched.headers),
	})
	if err != nil {
		return nil, wrapError(err, "select pypi package byte range")
	}
	fetched.status, fetched.headers, fetched.body = selected.Status, selected.Headers, selected.Body
	return fetched, nil
}
