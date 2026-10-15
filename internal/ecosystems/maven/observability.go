package maven

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
)

func (s *Service) publishCacheAccess(ctx context.Context, route Route, status, digest string) {
	if s == nil || s.events == nil {
		return
	}
	if err := events.Publish(ctx, s.events, events.CacheAccess{
		Kind:       string(route.Kind),
		Alias:      ecosystem.ScopedAlias(ecosystem.Maven, route.Alias),
		Repository: route.Repository,
		Reference:  route.Reference,
		Digest:     digest,
		Status:     status,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish maven cache access event failed", "error_kind", mavenErrorKind(err))
	}
}

func (s *Service) publishCacheStore(ctx context.Context, route Route, digest string, size int64) {
	if s == nil || s.events == nil {
		return
	}
	if err := events.Publish(ctx, s.events, events.CacheStore{
		Kind:       string(route.Kind),
		Alias:      ecosystem.ScopedAlias(ecosystem.Maven, route.Alias),
		Repository: route.Repository,
		Reference:  route.Reference,
		Digest:     digest,
		Size:       size,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish maven cache store event failed", "error_kind", mavenErrorKind(err))
	}
}

func (s *Service) publishUpstreamRequest(ctx context.Context, route Route, endpoint, method string, status int, duration time.Duration, err error) {
	if s == nil || s.events == nil {
		return
	}
	if status >= 400 {
		err = mavenHTTPStatusError(status)
	}
	if publishErr := events.Publish(ctx, s.events, events.UpstreamRequest{
		Alias:     ecosystem.ScopedAlias(ecosystem.Maven, route.Alias),
		Operation: "fetch",
		Registry:  mavenEndpointHost(endpoint),
		Method:    strings.ToUpper(strings.TrimSpace(method)),
		Path:      route.UpstreamTail,
		Status:    status,
		Attempts:  1,
		Duration:  duration,
		Error:     mavenErrorKind(err),
	}); publishErr != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish maven upstream request event failed", "error_kind", mavenErrorKind(publishErr))
	}
}

func (s *Service) publishUpstreamFailover(ctx context.Context, route Route, endpoint, method string, status int, requestErr error, hasNext bool) {
	if s == nil || s.events == nil {
		return
	}
	if status >= 400 {
		requestErr = mavenHTTPStatusError(status)
	}
	if err := events.Publish(ctx, s.events, events.UpstreamFailover{
		Alias:     ecosystem.ScopedAlias(ecosystem.Maven, route.Alias),
		Operation: strings.ToUpper(strings.TrimSpace(method)),
		Registry:  mavenEndpointHost(endpoint),
		Error:     mavenErrorKind(requestErr),
		HasNext:   hasNext,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish maven upstream failover event failed", "error_kind", mavenErrorKind(err))
	}
}

func mavenEndpointHost(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "unknown"
	}
	if host := parsed.Hostname(); host != "" {
		return host
	}
	return "unknown"
}

func mavenErrorKind(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	if networkErr, ok := errors.AsType[net.Error](err); ok {
		if networkErr.Timeout() {
			return "timeout"
		}
		return "network"
	}
	if statusErr, ok := errors.AsType[mavenHTTPStatusError](err); ok {
		return "http_status_" + strconv.Itoa(int(statusErr))
	}
	return "error"
}

type mavenHTTPStatusError int

func (e mavenHTTPStatusError) Error() string { return "upstream returned HTTP status" }
