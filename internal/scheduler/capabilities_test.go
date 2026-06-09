//nolint:testpackage // Tests cover unexported runtime capability helpers.
package scheduler

import (
	"context"
	"testing"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

type testRuntime struct {
	name string
}

func (r testRuntime) Name() string {
	return r.name
}

type testCleanerRuntime struct {
	testRuntime
}

func (r testCleanerRuntime) Cleanup(context.Context) (*ecosystem.CleanupReport, error) {
	return &ecosystem.CleanupReport{Ecosystem: r.Name()}, nil
}

func TestRuntimeCapabilitiesCollectsMatchingRuntimes(t *testing.T) {
	var nilRuntime ecosystem.Runtime
	runtime := NewRuntime(RuntimeDependencies{
		Runtimes: collectionlist.NewList[ecosystem.Runtime](
			nilRuntime,
			testRuntime{name: "plain"},
			testCleanerRuntime{testRuntime: testRuntime{name: "go"}},
		),
	})

	cleaners := runtimeCapabilities[ecosystem.Cleaner](runtime)
	if cleaners.Len() != 1 {
		t.Fatalf("expected 1 cleaner, got %d", cleaners.Len())
	}
	if got := cleaners.Values()[0].Name(); got != "go" {
		t.Fatalf("expected cleaner name go, got %q", got)
	}
}

func TestRuntimeCapabilityByNameSearchModes(t *testing.T) {
	runtime := NewRuntime(RuntimeDependencies{
		Runtimes: collectionlist.NewList[ecosystem.Runtime](
			testRuntime{name: "go"},
			testCleanerRuntime{testRuntime: testRuntime{name: "go"}},
		),
	})

	if _, ok := namedRuntimeCapability[ecosystem.Cleaner](runtime, "go", matchRuntimeNameExact, stopAfterRuntimeMatch); ok {
		t.Fatal("expected stopAfterRuntimeMatch to stop on the first named runtime without a cleaner")
	}

	cleaner, ok := namedRuntimeCapability[ecosystem.Cleaner](runtime, "go", matchRuntimeNameExact, continueAfterRuntimeMatch)
	if !ok {
		t.Fatal("expected continueAfterRuntimeMatch to find the later cleaner")
	}
	if got := cleaner.Name(); got != "go" {
		t.Fatalf("expected cleaner name go, got %q", got)
	}
}

func TestFindRuntimeByNameSupportsFoldedMatching(t *testing.T) {
	runtime := NewRuntime(RuntimeDependencies{
		Runtimes: collectionlist.NewList[ecosystem.Runtime](
			testRuntime{name: "Go"},
		),
	})

	found, ok := runtime.findRuntimeByName("go", matchRuntimeNameFold)
	if !ok {
		t.Fatal("expected folded runtime match")
	}
	if got := found.Name(); got != "Go" {
		t.Fatalf("expected runtime name Go, got %q", got)
	}
}
