package container

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryMetadataAdmissionCancelsBeforeLoading(t *testing.T) {
	endpoint := NewRegistryEndpoint(nil, nil, nil, nil, nil)
	if err := endpoint.metadataGate.Acquire(t.Context(), 4); err != nil {
		t.Fatal(err)
	}
	defer endpoint.metadataGate.Release(4)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var called atomic.Bool
	done := make(chan *registryOutput, 1)
	go func() {
		done <- endpoint.metadataResponse(ctx, func() (*registryOutput, func() error) {
			called.Store(true)
			return nil, nil
		})
	}()
	cancel()
	select {
	case result := <-done:
		if result == nil || called.Load() {
			t.Fatal("canceled admission loaded metadata or returned no error response")
		}
	case <-time.After(time.Second):
		t.Fatal("metadata admission did not observe cancellation")
	}
}

func TestRegistryMetadataAfterCallbackRunsAfterAdmissionRelease(t *testing.T) {
	endpoint := NewRegistryEndpoint(nil, nil, nil, nil, nil)
	var acquired atomic.Bool
	endpoint.metadataResponse(t.Context(), func() (*registryOutput, func() error) {
		return &registryOutput{}, func() error {
			if endpoint.metadataGate.TryAcquire(4) {
				acquired.Store(true)
				endpoint.metadataGate.Release(4)
			}
			return nil
		}
	})
	if !acquired.Load() {
		t.Fatal("after callback ran before metadata admission was released")
	}
}
