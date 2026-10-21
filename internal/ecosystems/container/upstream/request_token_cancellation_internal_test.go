package upstream

import (
	"context"
	"errors"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"
)

func TestFetchTokenCanceledOwnerAllowsHealthyFollowerToFinish(t *testing.T) {
	fixture := newFetchTokenCoalescingFixture(t)
	ownerCtx, cancelOwner := context.WithCancel(t.Context())
	defer cancelOwner()
	ownerResult := make(chan tokenCancellationResult, 1)
	go func() {
		token, err := fixture.client.fetchToken(ownerCtx, fixture.runtime, fixture.challenge, "")
		ownerResult <- tokenCancellationResult{token: token, err: err}
	}()
	waitForFirstTokenRequest(t, fixture.firstRequest)

	followerResult := make(chan tokenCancellationResult, 1)
	go func() {
		token, err := fixture.client.fetchToken(t.Context(), fixture.runtime, fixture.challenge, "")
		followerResult <- tokenCancellationResult{token: token, err: err}
	}()
	waitForTokenCancellationFlight(t)
	cancelOwner()

	select {
	case got := <-ownerResult:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled token owner error = %v, want context canceled", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled token owner did not return")
	}
	fixture.release()

	select {
	case got := <-followerResult:
		if got.err != nil {
			t.Fatalf("healthy token follower: %v", got.err)
		}
		if got.token != coalescedCredentialValue() {
			t.Fatalf("follower token = %q, want %q", got.token, coalescedCredentialValue())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("healthy token follower did not finish")
	}
	if got := fixture.requests.Load(); got != 1 {
		t.Fatalf("token endpoint requests = %d, want 1", got)
	}
}

type tokenCancellationResult struct {
	token string
	err   error
}

func waitForTokenCancellationFlight(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if tokenCancellationWaiterCount(t) >= 2 {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("token follower did not join the in-flight refresh")
}

func tokenCancellationWaiterCount(t *testing.T) int {
	t.Helper()
	var stacks strings.Builder
	if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
		t.Fatalf("write goroutine profile: %v", err)
	}
	waiters := 0
	for goroutine := range strings.SplitSeq(stacks.String(), "\n\n") {
		if strings.Contains(goroutine, ".fetchToken(") && strings.Contains(goroutine, "singleflight") {
			waiters++
		}
	}
	return waiters
}
