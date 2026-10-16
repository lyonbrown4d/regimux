//nolint:testpackage // These regressions inject private fill-pool state while exercising public Get.
package maven

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/stretchr/testify/require"
)

const (
	asyncFillFirstTail  = "com/acme/demo/1.0/demo-1.0.pom"
	asyncFillSecondTail = "com/acme/other/1.0/other-1.0.pom"
	asyncFillSecondPOM  = `<project><modelVersion>4.0.0</modelVersion><groupId>com.acme</groupId><artifactId>other</artifactId><version>1.0</version></project>`
)

var asyncFillFirstTag = meta.TagKey{Alias: "central", Repository: "com/acme/demo/1.0", Reference: "demo-1.0.pom"}
var asyncFillSecondTag = meta.TagKey{Alias: "central", Repository: "com/acme/other/1.0", Reference: "other-1.0.pom"}

func TestMavenGetRenewsDistributedLeaseUntilBlockedWriteCommits(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := newAsyncUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		if _, err := io.WriteString(w, slowWritePOM); err != nil {
			t.Errorf("write upstream POM: %v", err)
		}
	})
	local, objects := newAsyncBlockingStore(t)
	locker := newRenewalTestLocker()
	pools := worker.NewPoolsConfig(0, 1, slog.New(slog.DiscardHandler))
	t.Cleanup(pools.Close)
	fills := artifactcache.NewFillTracker(
		artifactcache.WithFillLocker(locker),
		artifactcache.WithFillLeaseScheduler(pools.LeasePool()),
		artifactcache.WithFillLeaseTTL(40*time.Millisecond),
	)
	service, metadata := newAsyncFillService(t, upstream.URL, objects, fills)
	locker.lease.commitVisible = func() bool {
		_, ok, lookupErr := metadata.Tag(context.Background(), asyncFillFirstTag)
		return lookupErr == nil && ok
	}
	t.Cleanup(objects.releasePut)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	response, err := service.Get(ctx, Request{Alias: "central", Tail: asyncFillFirstTail})
	assertAsyncMavenResponse(t, response, err, slowWritePOM)
	waitAsyncFillSignal(t, objects.entered, "blocked object Put")
	waitAsyncFillSignal(t, locker.acquired, "distributed lease acquisition")
	waitAsyncFillSignal(t, locker.lease.extended, "first lease renewal")
	waitAsyncFillSignal(t, locker.lease.extended, "second lease renewal")
	select {
	case <-locker.lease.released:
		t.Fatal("distributed lease released while object Put was blocked")
	default:
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls while Put blocked = %d, want 1", got)
	}
	objects.releasePut()
	waitAsyncFillSignal(t, locker.lease.released, "distributed lease release")
	if !locker.lease.commitVisibleAtRelease {
		t.Fatal("distributed lease released before cache metadata committed")
	}
	assertAsyncMavenCache(t, metadata, local, asyncFillFirstTag, slowWritePOM)
}

func TestMavenGetBypassesSaturatedFillPoolAndStillValidates(t *testing.T) {
	for _, tc := range []struct {
		name       string
		secondBody string
		wantErr    bool
	}{
		{name: "valid", secondBody: asyncFillSecondPOM},
		{name: "invalid POM", secondBody: "<definitely-not-a-pom>", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var firstCalls, secondCalls atomic.Int32
			upstream := newAsyncTwoArtifactUpstream(t, tc.secondBody, &firstCalls, &secondCalls)
			local, objects := newAsyncBlockingStore(t)
			service, metadata := newAsyncFillService(t, upstream.URL, objects, nil)
			pool, err := newMavenFillPool(1)
			require.NoError(t, err)
			service.fillPool = pool
			t.Cleanup(objects.releasePut)
			firstCtx, cancelFirst := context.WithTimeout(t.Context(), 2*time.Second)
			first, firstErr := service.Get(firstCtx, Request{Alias: "central", Tail: asyncFillFirstTail})
			cancelFirst()
			assertAsyncMavenResponse(t, first, firstErr, slowWritePOM)
			waitAsyncFillSignal(t, objects.entered, "first artifact object Put")
			secondCtx, cancelSecond := context.WithTimeout(t.Context(), 500*time.Millisecond)
			second, secondErr := service.Get(secondCtx, Request{Alias: "central", Tail: asyncFillSecondTail})
			cancelSecond()
			assertAsyncMavenGetResult(t, second, secondErr, tc.wantErr)
			require.Equal(t, int32(1), firstCalls.Load())
			require.Equal(t, int32(1), secondCalls.Load())
			require.Equal(t, int32(1), objects.puts.Load(), "second request must bypass the full pool")
			select {
			case <-objects.release:
				t.Fatal("first Put was released before second Get completed")
			default:
			}
			_, ok, tagErr := metadata.Tag(t.Context(), asyncFillSecondTag)
			require.NoError(t, tagErr)
			require.False(t, ok, "bypassed request must not publish cache metadata")
			objects.releasePut()
			assertAsyncMavenCache(t, metadata, local, asyncFillFirstTag, slowWritePOM)
		})
	}
}
