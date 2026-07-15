package scheduler_test

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/scheduler"
)

type schedulerJobContextKey struct{}

type countingLocker struct {
	lockCalls atomic.Int32
}

func (l *countingLocker) Lock(context.Context, string) (gocron.Lock, error) {
	l.lockCalls.Add(1)
	return countingLock{}, nil
}

type countingLock struct{}

func (countingLock) Unlock(context.Context) error {
	return nil
}

func TestRegisterDurationJobBuildsOptionsAndStartsImmediately(t *testing.T) {
	cron := newTestScheduler(t)
	parentCtx := context.WithValue(context.Background(), schedulerJobContextKey{}, "parent")
	done := make(chan struct{})
	receivedValue := make(chan string, 1)

	job, err := scheduler.RegisterDurationJob(
		parentCtx,
		cron,
		time.Hour,
		func(ctx context.Context) error {
			value, ok := ctx.Value(schedulerJobContextKey{}).(string)
			if !ok {
				t.Fatal("expected scheduler context value")
			}
			receivedValue <- value
			close(done)
			return nil
		},
		scheduler.JobOptions{
			Name:             "regimux.test.duration",
			Tags:             []string{"maintenance", "probe"},
			StartImmediately: true,
		},
	)
	if err != nil {
		t.Fatalf("register duration job: %v", err)
	}
	if got := job.Name(); got != "regimux.test.duration" {
		t.Fatalf("expected job name regimux.test.duration, got %q", got)
	}
	if got, want := job.Tags(), []string{"maintenance", "probe"}; !slices.Equal(got, want) {
		t.Fatalf("expected tags %v, got %v", want, got)
	}
	schedule, ok := job.Schedule().(gocron.DurationJobSchedule)
	if !ok {
		t.Fatalf("expected duration schedule, got %T", job.Schedule())
	}
	if schedule.Duration != time.Hour {
		t.Fatalf("expected duration %s, got %s", time.Hour, schedule.Duration)
	}

	cron.Start()
	waitForJobRun(t, done)
	if got := <-receivedValue; got != "parent" {
		t.Fatalf("expected task context value parent, got %q", got)
	}
}

func TestRegisterImmediateJobDisablesDistributedLocker(t *testing.T) {
	locker := &countingLocker{}
	cron := newTestScheduler(t, gocron.WithDistributedLocker(locker))
	done := make(chan struct{})
	localJob := false

	job, err := scheduler.RegisterImmediateJob(
		context.Background(),
		cron,
		func(context.Context) error {
			close(done)
			return nil
		},
		scheduler.JobOptions{
			Name:        "regimux.test.immediate",
			Tags:        []string{"manual"},
			Distributed: &localJob,
		},
	)
	if err != nil {
		t.Fatalf("register immediate job: %v", err)
	}
	if got, want := job.Tags(), []string{"manual"}; !slices.Equal(got, want) {
		t.Fatalf("expected tags %v, got %v", want, got)
	}

	cron.Start()
	waitForJobRun(t, done)
	if got := locker.lockCalls.Load(); got != 0 {
		t.Fatalf("expected distributed locker to be disabled, got %d lock calls", got)
	}
}

func newTestScheduler(t *testing.T, options ...gocron.SchedulerOption) gocron.Scheduler {
	t.Helper()
	cron, err := gocron.NewScheduler(options...)
	if err != nil {
		t.Fatalf("create scheduler: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := cron.ShutdownWithContext(ctx); err != nil {
			t.Fatalf("shutdown scheduler: %v", err)
		}
	})
	return cron
}

func waitForJobRun(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for job to run")
	}
}
