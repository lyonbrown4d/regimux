package worker_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

func TestPoolsRunIOAllSettledUsesPoolLimit(t *testing.T) {
	pools := worker.NewPoolsConfig(2, 0, slog.New(slog.DiscardHandler))
	t.Cleanup(pools.Close)

	started := make(chan struct{}, 4)
	release := make(chan struct{})
	tasks := blockingTasks(4, started, release)

	done := make(chan error, 1)
	go func() {
		done <- pools.RunIOAllSettled(context.Background(), tasks)
	}()

	waitForWorkerTaskStart(t, started)
	waitForWorkerTaskStart(t, started)
	select {
	case <-started:
		t.Fatal("third worker task started above IO pool limit")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	waitForWorkerTasks(t, done)
}

func TestPoolsRunIOAllSettledWithoutPoolRunsSequentially(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	tasks := collectionlist.NewList(
		func(context.Context) error {
			close(firstStarted)
			<-releaseFirst
			return nil
		},
		func(context.Context) error {
			close(secondStarted)
			return nil
		},
	)

	var pools *worker.Pools
	done := make(chan error, 1)
	go func() {
		done <- pools.RunIOAllSettled(context.Background(), tasks)
	}()

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first worker task")
	}
	select {
	case <-secondStarted:
		t.Fatal("second worker task started before the first completed")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseFirst)

	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second worker task")
	}
	waitForWorkerTasks(t, done)
}

func blockingTasks(
	count int,
	started chan<- struct{},
	release <-chan struct{},
) *collectionlist.List[func(context.Context) error] {
	tasks := collectionlist.NewListWithCapacity[func(context.Context) error](count)
	for range count {
		tasks.Add(func(ctx context.Context) error {
			started <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}
	return tasks
}

func waitForWorkerTaskStart(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker task")
	}
}

func waitForWorkerTasks(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run worker tasks: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker tasks")
	}
}
