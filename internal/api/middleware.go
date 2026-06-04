package api

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/gofiber/fiber/v3/middleware/etag"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/pprof"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/lyonbrown4d/regimux/internal/config"
	slogfiber "github.com/samber/slog-fiber"
)

const (
	registryPathPrefix = "/v2"
	adminPathPrefix    = "/admin"
)

func installFiberMiddleware(app *fiber.App, cfg config.ServerMiddlewareConfig, logger *slog.Logger) {
	logger = apiLogger(logger, "api.middleware")
	logger.Info("installing fiber middleware",
		"request_id", cfg.RequestID.Enabled,
		"request_logger", cfg.RequestLogger.Enabled,
		"healthcheck", cfg.Healthcheck.Enabled,
		"etag", cfg.ETag.Enabled,
		"security_headers", cfg.SecurityHeaders.Enabled,
		"compress", cfg.Compress.Enabled,
		"rate_limit", cfg.RateLimit.Enabled,
		"csrf", cfg.CSRF.Enabled,
		"pprof", cfg.Pprof.Enabled,
	)
	app.Use(recover.New())
	installRequestID(app, cfg.RequestID)
	installRequestLogger(app, cfg.RequestLogger, cfg.RequestID.Header, logger)
	installHealthcheck(app, cfg.Healthcheck)
	installPprof(app, cfg.Pprof)
	installSecurityHeaders(app, cfg.SecurityHeaders)
	installCompress(app, cfg.Compress)
	installETag(app, cfg.ETag)
	installRateLimit(app, cfg.RateLimit)
	installCSRF(app, cfg.CSRF)
	logger.Info("fiber middleware installed")
}

func installRequestID(app *fiber.App, cfg config.MiddlewareRequestIDConfig) {
	if !cfg.Enabled {
		return
	}
	app.Use(requestid.New(requestid.Config{Header: cfg.Header}))
}

func installRequestLogger(app *fiber.App, cfg config.MiddlewareRequestLoggerConfig, requestIDHeader string, logger *slog.Logger) {
	if !cfg.Enabled {
		return
	}
	headerKey := strings.TrimSpace(requestIDHeader)
	if headerKey == "" {
		headerKey = "X-Request-ID"
	}
	slogfiber.RequestIDContextKey = "requestid"
	slogfiber.RequestIDHeaderKey = headerKey

	app.Use(slogfiber.NewWithConfig(
		apiLogger(logger, "api.request"),
		slogfiber.Config{
			DefaultLevel:       slog.LevelInfo,
			ClientErrorLevel:   slog.LevelWarn,
			ServerErrorLevel:   slog.LevelError,
			WithRequestID:      true,
			WithUserAgent:      true,
			WithRequestBody:    true,
			WithResponseBody:   false,
			WithRequestHeader:  true,
			WithResponseHeader: false,
			WithTraceID:        false,
			WithSpanID:         false,
		},
	))
}

func installHealthcheck(app *fiber.App, cfg config.MiddlewareHealthcheckConfig) {
	if !cfg.Enabled {
		return
	}
	handler := healthcheck.New(healthcheck.Config{ResponseFormat: healthcheck.FormatJSON})
	app.Get(cfg.LivenessPath, handler)
	app.Get(cfg.ReadinessPath, handler)
}

func apiLogger(logger *slog.Logger, component string) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger.With("component", component)
}

func installPprof(app *fiber.App, cfg config.MiddlewarePprofConfig) {
	if !cfg.Enabled {
		return
	}
	app.Use(pprof.New(pprof.Config{Prefix: cfg.Prefix}))
}

func installSecurityHeaders(app *fiber.App, cfg config.MiddlewareSecurityHeadersConfig) {
	if !cfg.Enabled {
		return
	}
	app.Use(helmet.New(helmet.Config{
		Next:                      skipRegistryPath,
		ContentSecurityPolicy:     cfg.ContentSecurityPolicy,
		CrossOriginEmbedderPolicy: cfg.CrossOriginEmbedderPolicy,
		HSTSMaxAge:                cfg.HSTSMaxAge,
	}))
}

func installCompress(app *fiber.App, cfg config.MiddlewareCompressConfig) {
	if !cfg.Enabled {
		return
	}
	app.Use(compress.New(compress.Config{
		Next:  skipRegistryPath,
		Level: fiberCompressLevel(cfg.Level),
	}))
}

func installETag(app *fiber.App, cfg config.MiddlewareToggleConfig) {
	if !cfg.Enabled {
		return
	}
	app.Use(etag.New(etag.Config{Next: skipRegistryPath}))
}

func installRateLimit(app *fiber.App, cfg config.MiddlewareRateLimitConfig) {
	if !cfg.Enabled {
		return
	}
	app.Use(limiter.New(limiter.Config{
		Next:       skipRateLimit,
		Max:        cfg.Max,
		Expiration: cfg.Expiration,
	}))
}

func installCSRF(app *fiber.App, cfg config.MiddlewareCSRFConfig) {
	if !cfg.Enabled {
		return
	}
	app.Use(csrf.New(csrf.Config{
		Next:           skipNonAdminPath,
		Extractor:      extractors.FromForm("_csrf"),
		IdleTimeout:    cfg.IdleTimeout,
		CookieName:     cfg.CookieName,
		CookiePath:     adminPathPrefix,
		CookieSecure:   cfg.CookieSecure,
		CookieHTTPOnly: true,
		TrustedOrigins: cfg.TrustedOrigins,
	}))
}

func fiberCompressLevel(level string) compress.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "disabled":
		return compress.LevelDisabled
	case "best_speed":
		return compress.LevelBestSpeed
	case "best_compression":
		return compress.LevelBestCompression
	default:
		return compress.LevelDefault
	}
}

func skipRegistryPath(c fiber.Ctx) bool {
	return isRegistryPath(c.Path())
}

func skipNonAdminPath(c fiber.Ctx) bool {
	return !isAdminPath(c.Path())
}

func skipRateLimit(c fiber.Ctx) bool {
	path := c.Path()
	if path == credentialExchangePath() {
		return false
	}
	return !isAdminPath(path) || isSafeMethod(c.Method())
}

func credentialExchangePath() string {
	return strings.Join([]string{"", "auth", "token"}, "/")
}

func isRegistryPath(path string) bool {
	return path == registryPathPrefix || strings.HasPrefix(path, registryPathPrefix+"/")
}

func isAdminPath(path string) bool {
	return path == adminPathPrefix || strings.HasPrefix(path, adminPathPrefix+"/")
}

func isSafeMethod(method string) bool {
	switch method {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions, fiber.MethodTrace:
		return true
	default:
		return false
	}
}
