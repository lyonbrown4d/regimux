package pypi

import (
	"context"
	"net/url"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
)

func (s *Service) observePackageHeaders(ctx context.Context, req Request, rawURL string, started time.Time, fetched *upstreamFetch, fetchErr error) {
	if s.events == nil {
		return
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	event := events.UpstreamRequest{Alias: ecosystem.ScopedAlias(ecosystem.PyPI, req.Alias), Operation: "pypi.package.headers", Registry: parsed.Scheme + "://" + parsed.Host, Method: methodOrGet(req.Method), Path: parsed.EscapedPath(), Attempts: 1, Duration: time.Since(started)}
	if fetched != nil {
		event.Status = fetched.status
		event.Size = contentLength(fetched.headers)
	}
	if fetchErr != nil {
		event.Error = "request_failed"
	}
	s.publishPackageEvent(ctx, event)
}

func (s *Service) publishPackageEvent(ctx context.Context, event events.Event) {
	if err := events.Publish(ctx, s.events, event); err != nil {
		s.logger.DebugContext(ctx, "publish pypi transfer event failed")
	}
}

func (s *Service) observePackageCache(ctx context.Context, alias, status string) {
	s.publishPackageEvent(ctx, events.CacheAccess{Kind: "pypi.package", Alias: ecosystem.ScopedAlias(ecosystem.PyPI, alias), Status: status})
}
