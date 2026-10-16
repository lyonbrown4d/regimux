package cache_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
)

func TestBlobAccessFanInDetachesCanceledOwnerForHealthyFollower(t *testing.T) {
	body := []byte("0123456789abcdef")
	digest := testDigestFor(body)
	fixture := newBlobAccessFanInFixture(t, digest, body, 2)
	ownerCtx, cancelOwner := context.WithCancel(t.Context())
	defer cancelOwner()
	ownerResult := startBlobAccessCaller(ownerCtx, fixture.proxy, blobAccessRequest(digest, http.MethodGet))
	waitForBlobStatBarrier(t, fixture.objects.entered, 1)
	followerResult := startBlobAccessCaller(t.Context(), fixture.proxy, blobAccessRequest(digest, http.MethodHead))
	waitForBlobStatBarrier(t, fixture.objects.entered, 1)
	fixture.objects.releaseStat()
	waitForBlobMetadataRead(t, fixture.metadata.blobReadEntered)
	waitForBlobTouchCallers(t, 2)
	cancelOwner()
	assertBlobAccessCallerCanceled(t, ownerResult)
	fixture.metadata.release()
	got := receiveBlobAccessCaller(t, followerResult, "healthy follower")
	assertBlobFanInResult(t, got, body)
	assertBlobAccessFanInCounts(t, fixture.metadata, 2)
}

func TestBlobAccessFanInPreservesMetadataErrorChain(t *testing.T) {
	body := []byte("0123456789abcdef")
	digest := testDigestFor(body)
	fixture := newBlobAccessFanInFixture(t, digest, body, 1)
	metadataErr := errors.New("metadata blob read failed")
	fixture.metadata.blobReadErr = metadataErr
	result := startBlobAccessCaller(t.Context(), fixture.proxy, blobAccessRequest(digest, http.MethodGet))
	waitForBlobStatBarrier(t, fixture.objects.entered, 1)
	fixture.objects.releaseStat()
	waitForBlobMetadataRead(t, fixture.metadata.blobReadEntered)
	waitForBlobTouchCallers(t, 1)
	fixture.metadata.release()
	got := receiveBlobAccessCaller(t, result, "failed blob access touch")
	if !errors.Is(got.err, metadataErr) {
		t.Fatalf("blob access metadata error = %v, want wrapped %v", got.err, metadataErr)
	}
}

func startBlobAccessCaller(ctx context.Context, proxy *cache.Proxy, req cache.BlobRequest) <-chan blobAccessFanInResult {
	result := make(chan blobAccessFanInResult, 1)
	go func() {
		value, err := proxy.Blobs().Get(ctx, req)
		result <- blobAccessFanInResult{request: req, result: value, err: err}
	}()
	return result
}

func receiveBlobAccessCaller(t *testing.T, result <-chan blobAccessFanInResult, label string) blobAccessFanInResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return", label)
		return blobAccessFanInResult{}
	}
}

func assertBlobAccessCallerCanceled(t *testing.T, result <-chan blobAccessFanInResult) {
	t.Helper()
	got := receiveBlobAccessCaller(t, result, "canceled blob access owner")
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("canceled owner error = %v, want context canceled", got.err)
	}
}

func waitForBlobMetadataRead(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("blob metadata touch did not start")
	}
}

func blobAccessRequest(digest, method string) cache.BlobRequest {
	return cache.BlobRequest{UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: method}
}
