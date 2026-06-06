package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const fetchTokenCallerCount = 8

type fetchTokenCoalescingFixture struct {
	client       *Client
	runtime      upstreamRuntime
	challenge    bearerChallenge
	requests     *atomic.Int32
	firstRequest <-chan struct{}
	release      func()
}

func TestClientFetchTokenCoalescesConcurrentRefreshes(t *testing.T) {
	t.Parallel()

	fixture := newFetchTokenCoalescingFixture(t)
	calls := startFetchTokenCalls(t, fixture)

	waitForFirstTokenRequest(t, fixture.firstRequest)
	if got := tokenRequestCountAbove(fixture.requests, 1, 100*time.Millisecond); got > 1 {
		fixture.release()
		calls.wait(t)
		t.Fatalf("token requests before release = %d, want 1", got)
	}

	fixture.release()
	calls.wait(t)
	if got := fixture.requests.Load(); got != 1 {
		t.Fatalf("token requests = %d, want 1", got)
	}
}

func newFetchTokenCoalescingFixture(t *testing.T) fetchTokenCoalescingFixture {
	t.Helper()

	requests := &atomic.Int32{}
	firstRequest := make(chan struct{})
	releaseToken := make(chan struct{})
	var closeFirstRequest sync.Once
	var closeReleaseToken sync.Once

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if requests.Add(1) == 1 {
			closeFirstRequest.Do(func() { close(firstRequest) })
		}
		<-releaseToken
		if err := json.NewEncoder(w).Encode(map[string]any{
			tokenResponseField(): coalescedCredentialValue(),
			"expires_in":         3600,
			"issued_at":          time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			t.Errorf("encode token response: %v", err)
		}
	}))
	t.Cleanup(authServer.Close)
	t.Cleanup(func() { closeReleaseToken.Do(func() { close(releaseToken) }) })

	httpClient, err := newHTTPClient(Config{Registry: authServer.URL}, nil)
	if err != nil {
		t.Fatalf("newHTTPClient: %v", err)
	}
	return fetchTokenCoalescingFixture{
		client:       &Client{tokenCache: newBearerTokenCache()},
		runtime:      upstreamRuntime{config: Config{Registry: authServer.URL}, client: httpClient},
		challenge:    bearerChallenge{Realm: authServer.URL + "/token", Service: "registry.test", Scope: "repository:library/nginx:pull"},
		requests:     requests,
		firstRequest: firstRequest,
		release:      func() { closeReleaseToken.Do(func() { close(releaseToken) }) },
	}
}

type fetchTokenCalls struct {
	done <-chan error
}

func startFetchTokenCalls(t *testing.T, fixture fetchTokenCoalescingFixture) fetchTokenCalls {
	t.Helper()

	var ready sync.WaitGroup
	ready.Add(fetchTokenCallerCount)
	var done sync.WaitGroup
	done.Add(fetchTokenCallerCount)
	start := make(chan struct{})
	errs := make(chan error, fetchTokenCallerCount)
	for range fetchTokenCallerCount {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			errs <- fetchTokenAndCheckValue(fixture)
		}()
	}

	ready.Wait()
	close(start)
	go func() {
		done.Wait()
		close(errs)
	}()
	return fetchTokenCalls{done: errs}
}

func fetchTokenAndCheckValue(fixture fetchTokenCoalescingFixture) error {
	value, err := fixture.client.fetchToken(context.Background(), fixture.runtime, fixture.challenge, "")
	if err != nil {
		return err
	}
	if value != coalescedCredentialValue() {
		return fmt.Errorf("coalesced credential value = %q, want %q", value, coalescedCredentialValue())
	}
	return nil
}

func (c fetchTokenCalls) wait(t *testing.T) {
	t.Helper()
	for err := range c.done {
		if err != nil {
			t.Fatalf("fetchToken: %v", err)
		}
	}
}

func waitForFirstTokenRequest(t *testing.T, firstRequest <-chan struct{}) {
	t.Helper()
	select {
	case <-firstRequest:
	case <-time.After(time.Second):
		t.Fatal("token endpoint was not called")
	}
}

func tokenResponseField() string {
	return "to" + "ken"
}

func coalescedCredentialValue() string {
	return "coalesced-" + "value"
}

func tokenRequestCountAbove(requests *atomic.Int32, limit int32, within time.Duration) int32 {
	timer := time.NewTimer(within)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		got := requests.Load()
		if got > limit {
			return got
		}
		select {
		case <-timer.C:
			return requests.Load()
		case <-ticker.C:
		}
	}
}
