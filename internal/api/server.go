package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/arcgolabs/httpx"
	"github.com/arcgolabs/httpx/adapter"
	"github.com/arcgolabs/httpx/adapter/std"
)

type Server struct {
	listen string
	logger *slog.Logger

	adapter *std.Adapter
	runtime httpx.ServerRuntime
	errCh   chan error
	once    sync.Once
}

type Options struct {
	Listen      string
	PublicURL   string
	Logger      *slog.Logger
	Endpoints   []httpx.Endpoint
	PrintRoutes bool
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

	stdAdapter := std.New(nil, adapter.HumaOptions{
		Title:       "RegiMux",
		Version:     "dev",
		Description: "Read-only OCI / Docker Registry V2 multi-upstream proxy mirror gateway.",
		DocsPath:    "/docs",
		OpenAPIPath: "/openapi.json",
	})
	server := httpx.New(
		httpx.WithAdapter(stdAdapter),
		httpx.WithLogger(logger),
		httpx.WithPrintRoutes(opts.PrintRoutes),
	)
	for _, endpoint := range opts.Endpoints {
		server.RegisterOnly(endpoint)
	}

	return &Server{
		listen:  listen,
		logger:  logger,
		adapter: stdAdapter,
		runtime: server,
		errCh:   make(chan error, 1),
	}
}

func (s *Server) Start(ctx context.Context) error {
	if s == nil {
		return errors.New("api server is nil")
	}
	s.logger.Info("starting http server", "listen", s.listen)
	go func() {
		s.errCh <- s.runtime.Listen(s.listen)
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
		return shutdownErr
	}

	select {
	case err := <-s.errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server stopped: %w", err)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
