package worker_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/worker"
)

func TestRunAllReturnsTaskPanic(t *testing.T) {
	t.Parallel()

	pools := worker.NewPoolsConfig(1, 1, slog.New(slog.DiscardHandler))
	defer pools.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := worker.RunAll(ctx, pools.IOPool(), taskList{
		func(context.Context) error {
			panic("boom")
		},
	})
	if err == nil {
		t.Fatal("RunAll() error = nil, want panic error")
	}
	if !strings.Contains(err.Error(), "worker task panicked: boom") {
		t.Fatalf("RunAll() error = %q", err)
	}
}
