package api_test

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/api"
)

func TestServerStartReportsListenFailure(t *testing.T) {
	t.Parallel()

	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := listener.Close(); closeErr != nil {
			t.Errorf("close listener: %v", closeErr)
		}
	})

	server := api.NewServer(api.Options{Listen: listener.Addr().String(), Logger: testLogger()})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Start(ctx); err == nil {
		t.Fatal("Start() error = nil, want listen failure")
	}
}

func TestServerStopIsIdempotent(t *testing.T) {
	t.Parallel()

	server := api.NewServer(api.Options{Listen: "127.0.0.1:0", Logger: testLogger()})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := server.Stop(ctx); err != nil {
		t.Fatalf("first Stop() error = %v", err)
	}
	if err := server.Stop(ctx); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
