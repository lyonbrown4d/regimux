package scheduler_test

import (
	"context"
	"testing"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/scheduler"
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
	runtime := scheduler.NewRuntime(scheduler.RuntimeDependencies{
		Runtimes: collectionlist.NewList[ecosystem.Runtime](
			nilRuntime,
			testRuntime{name: "plain"},
			testCleanerRuntime{testRuntime: testRuntime{name: "go"}},
		),
	})

	cleaners := scheduler.RuntimeCapabilities[ecosystem.Cleaner](runtime)
	if cleaners.Len() != 1 {
		t.Fatalf("expected 1 cleaner, got %d", cleaners.Len())
	}
	if got := cleaners.Values()[0].Name(); got != "go" {
		t.Fatalf("expected cleaner name go, got %q", got)
	}
}

func TestRuntimeCapabilityByNameSearchModes(t *testing.T) {
	runtime := scheduler.NewRuntime(scheduler.RuntimeDependencies{
		Runtimes: collectionlist.NewList[ecosystem.Runtime](
			testRuntime{name: "go"},
			testCleanerRuntime{testRuntime: testRuntime{name: "go"}},
		),
	})

	_, ok := scheduler.NamedRuntimeCapability[ecosystem.Cleaner](
		runtime,
		"go",
		scheduler.MatchRuntimeNameExact,
		scheduler.StopAfterRuntimeMatch,
	)
	if ok {
		t.Fatal("expected stop mode to stop on the first named runtime without a cleaner")
	}

	cleaner, ok := scheduler.NamedRuntimeCapability[ecosystem.Cleaner](
		runtime,
		"go",
		scheduler.MatchRuntimeNameExact,
		scheduler.ContinueAfterRuntimeMatch,
	)
	if !ok {
		t.Fatal("expected continue mode to find the later cleaner")
	}
	if got := cleaner.Name(); got != "go" {
		t.Fatalf("expected cleaner name go, got %q", got)
	}
}

func TestFindRuntimeByNameSupportsFoldedMatching(t *testing.T) {
	runtime := scheduler.NewRuntime(scheduler.RuntimeDependencies{
		Runtimes: collectionlist.NewList[ecosystem.Runtime](testRuntime{name: "Go"}),
	})

	found, ok := runtime.FindRuntimeByName("go", scheduler.MatchRuntimeNameFold)
	if !ok {
		t.Fatal("expected folded runtime match")
	}
	if got := found.Name(); got != "Go" {
		t.Fatalf("expected runtime name Go, got %q", got)
	}
}
