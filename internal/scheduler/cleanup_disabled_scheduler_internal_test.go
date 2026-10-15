package scheduler

import "testing"

func TestDisabledSchedulerKeepsManualCleanupExecutorWithoutRecurringJobs(t *testing.T) {
	runtime, _, _ := newSharedCleanupRuntime(t)
	runtime.cfg.Scheduler.Enabled = false

	if err := runtime.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if runtime.scheduler == nil {
		t.Fatal("manual cleanup scheduler is nil while cleanup service is configured")
	}
	if jobs := runtime.scheduler.Jobs(); len(jobs) != 0 {
		t.Fatalf("disabled scheduler registered %d recurring jobs, want none", len(jobs))
	}
}
