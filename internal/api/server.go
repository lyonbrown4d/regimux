package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/samber/lo"
	"log/slog"
	"net"
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
)

type Server struct {
	listen string
	logger *slog.Logger

	adapter     *fiberadapter.Adapter
	runtime     httpx.ServerRuntime
	readyCh     chan fiber.ListenData
	doneCh      chan struct{}
	startOnce   sync.Once
	readyOnce   sync.Once
	stopOnce    sync.Once
	mu          sync.RWMutex
	started     bool
	listenErr   error
	shutdownErr error
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
		listen = ":8080"
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
		readyCh: make(chan fiber.ListenData, 1),
		doneCh:  make(chan struct{}),
	}
}

func (s *Server) Start(ctx context.Context) error {
	s.startOnce.Do(func() {
		s.mu.Lock()
		s.started = true
		s.mu.Unlock()
		s.adapter.Router().Hooks().OnListen(func(data fiber.ListenData) error {
			s.readyOnce.Do(func() { s.readyCh <- data })
			return nil
		})
		go func() {
			err := s.runtime.Listen(s.listen)
			s.mu.Lock()
			s.listenErr = err
			s.mu.Unlock()
			close(s.doneCh)
		}()
	})
	select {
	case data := <-s.readyCh:
		return waitUntilServing(ctx, data)
	case <-s.doneCh:
		if err := s.listenResult(); err != nil {
			return err
		}
		return errors.New("API server stopped before listening")
	case <-ctx.Done():
		return fmt.Errorf("start API server: %w", ctx.Err())
	}
}

func (s *Server) Stop(ctx context.Context) error {
	if !s.hasStarted() {
		return nil
	}
	s.stopOnce.Do(func() {
		err := s.runtime.Shutdown()
		s.mu.Lock()
		s.shutdownErr = err
		s.mu.Unlock()
	})
	if err := s.shutdownResult(); err != nil {
		return err
	}
	select {
	case <-s.doneCh:
		return s.listenResult()
	case <-ctx.Done():
		return fmt.Errorf("stop API server: %w", ctx.Err())
	}
}

func (s *Server) hasStarted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started
}

func (s *Server) listenResult() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listenErr
}

func (s *Server) shutdownResult() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shutdownErr
}

func (s *Server) HasRoute(method, path string) bool {
	app := s.adapter.Router()
	fiberConfig := app.Config()
	return lo.SomeBy(app.GetRoutes(true), func(route fiber.Route) bool {
		return route.Method == method && fiber.RoutePatternMatch(path, route.Path, fiberConfig)
	})
}

func waitUntilServing(ctx context.Context, data fiber.ListenData) error {
	host := data.Host
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	url := "http://" + net.JoinHostPort(host, data.Port) + "/__regimux_readiness__"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("create API readiness request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("wait for API server readiness: %w", err)
	}
	if err := response.Body.Close(); err != nil {
		return fmt.Errorf("close API readiness response: %w", err)
	}
	return nil
}
