package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/httpx"
	"github.com/arcgolabs/httpx/adapter"
	fiberadapter "github.com/arcgolabs/httpx/adapter/fiber"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/samber/oops"
)

type Server struct {
	listen string
	logger *slog.Logger

	adapter *fiberadapter.Adapter
	runtime httpx.ServerRuntime
	errCh   chan error
	once    sync.Once
}

type Options struct {
	Listen       string
	PublicURL    string
	Logger       *slog.Logger
	Endpoints    *collectionlist.List[httpx.Endpoint]
	FiberRoutes  *collectionlist.List[FiberRoute]
	Views        fiber.Views
	Metrics      *observability.Metrics
	Auth         *auth.Service
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	Middleware   config.ServerMiddlewareConfig
	PrintRoutes  bool
}

func NewServer(opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	listen := opts.Listen
	if listen == "" {
		listen = ":5000"
	}

	fiberApp := fiber.New(fiber.Config{
		ReadTimeout:  opts.ReadTimeout,
		WriteTimeout: opts.WriteTimeout,
		IdleTimeout:  opts.IdleTimeout,
		Views:        opts.Views,
	})
	fiberApp.Hooks().OnPreStartupMessage(func(sm *fiber.PreStartupMessageData) error {
		sm.PreventDefault = true
		return nil
	})
	installFiberMiddleware(fiberApp, opts.Middleware, logger)
	if opts.Metrics != nil {
		fiberApp.Get("/metrics", adaptor.HTTPHandler(opts.Metrics.Handler()))
	}
	routeRegistrations := opts.FiberRoutes
	if routeRegistrations != nil {
		routeRegistrations.Range(func(_ int, route FiberRoute) bool {
			route.RegisterFiber(fiberApp)
			return true
		})
	}
	if opts.Auth != nil {
		opts.Auth.RegisterFiber(fiberApp)
	}

	fiberAdapter := fiberadapter.New(fiberApp, adapter.HumaOptions{
		Title:       "RegiMux",
		Version:     "dev",
		Description: "Read-only OCI / Docker Registry V2 multi-upstream proxy mirror gateway.",
		DocsPath:    "/docs",
		OpenAPIPath: "/openapi.json",
	})
	server := httpx.New(
		httpx.WithAdapter(fiberAdapter),
		httpx.WithLogger(logger),
		httpx.WithPrintRoutes(opts.PrintRoutes),
	)
	endpoints := opts.Endpoints
	if endpoints == nil {
		endpoints = collectionlist.NewList[httpx.Endpoint]()
	}
	endpoints.Range(func(_ int, endpoint httpx.Endpoint) bool {
		server.RegisterOnly(endpoint)
		return true
	})

	return &Server{
		listen:  listen,
		logger:  logger,
		adapter: fiberAdapter,
		runtime: server,
		errCh:   make(chan error, 1),
	}
}

func (s *Server) Start(ctx context.Context) error {
	if s == nil {
		return oops.Errorf("api server is nil")
	}
	s.logger.Info("starting http server", "listen", s.listen)
	go func() {
		if err := s.runtime.Listen(s.listen); err != nil {
			s.errCh <- oops.Wrapf(err, "listen http server")
			return
		}
		s.errCh <- nil
	}()
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}

	var shutdownErr error
	s.once.Do(func() {
		s.logger.Info("stopping http server", "listen", s.listen)
		shutdownErr = s.runtime.Shutdown()
	})
	if shutdownErr != nil {
		return oops.Wrapf(shutdownErr, "shutdown http server")
	}

	select {
	case err := <-s.errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return oops.Wrapf(err, "http server stopped")
		}
	case <-ctx.Done():
		return oops.Wrapf(ctx.Err(), "wait for http server shutdown")
	}
	s.logger.Info("http server stopped", "listen", s.listen)
	return nil
}

func (s *Server) HasRoute(method, path string) bool {
	if s == nil || s.runtime == nil {
		return false
	}
	_, ok := s.runtime.MatchRoute(method, path)
	return ok
}
