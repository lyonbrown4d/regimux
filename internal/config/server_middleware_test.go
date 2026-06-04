package config_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestLoadDefaultsIncludeServerMiddleware(t *testing.T) {
	cfg := loadDefaultConfig(t)
	assertDefaultServerMiddleware(t, cfg.Server.Middleware)
}

func assertDefaultServerMiddleware(t *testing.T, middleware config.ServerMiddlewareConfig) {
	t.Helper()
	assertDefaultRequestID(t, middleware.RequestID)
	assertDefaultHealthcheck(t, middleware.Healthcheck)
	assertDefaultResponseMiddleware(t, middleware)
	assertOptInMiddleware(t, middleware)
}

func assertDefaultRequestID(t *testing.T, requestID config.MiddlewareRequestIDConfig) {
	t.Helper()
	if !requestID.Enabled || requestID.Header != "X-Request-ID" {
		t.Fatalf("unexpected request id middleware defaults: %#v", requestID)
	}
}

func assertDefaultHealthcheck(t *testing.T, healthcheck config.MiddlewareHealthcheckConfig) {
	t.Helper()
	if !healthcheck.Enabled || healthcheck.LivenessPath != "/livez" || healthcheck.ReadinessPath != "/readyz" {
		t.Fatalf("unexpected healthcheck middleware defaults: %#v", healthcheck)
	}
}

func assertDefaultResponseMiddleware(t *testing.T, middleware config.ServerMiddlewareConfig) {
	t.Helper()
	if !middleware.ETag.Enabled || !middleware.SecurityHeaders.Enabled || !middleware.Compress.Enabled {
		t.Fatalf("unexpected response middleware defaults: %#v", middleware)
	}
	if middleware.SecurityHeaders.CrossOriginEmbedderPolicy != "unsafe-none" {
		t.Fatalf("unexpected COEP default: %#v", middleware.SecurityHeaders)
	}
}

func assertOptInMiddleware(t *testing.T, middleware config.ServerMiddlewareConfig) {
	t.Helper()
	if middleware.RequestLogger.Enabled || middleware.RateLimit.Enabled || middleware.CSRF.Enabled || middleware.Pprof.Enabled {
		t.Fatalf("unsafe/debug middleware should be opt-in by default: %#v", middleware)
	}
}
