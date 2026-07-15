package scheduler

import (
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

type runtimeCapabilitySearchMode uint8

const (
	continueAfterRuntimeMatch runtimeCapabilitySearchMode = iota
	stopAfterRuntimeMatch
)

type runtimeNameMatcher func(runtimeName, lookup string) bool

func matchRuntimeNameExact(runtimeName, lookup string) bool {
	return runtimeName == lookup
}

func matchRuntimeNameFold(runtimeName, lookup string) bool {
	return strings.EqualFold(runtimeName, lookup)
}

func (r *Runtime) jobProviders() *collectionlist.List[ecosystem.JobProvider] {
	return runtimeCapabilities[ecosystem.JobProvider](r)
}

func runtimeCapability[T any](runtime ecosystem.Runtime) (T, bool) {
	var zero T
	if runtime == nil {
		return zero, false
	}
	capability, ok := any(runtime).(T)
	if !ok {
		return zero, false
	}
	return capability, true
}

func firstRuntimeCapability[T any](r *Runtime) (T, bool) {
	var zero T
	if r == nil || r.runtimes == nil {
		return zero, false
	}
	runtime, found := collectionlist.FindList(r.runtimes, func(_ int, runtime ecosystem.Runtime) bool {
		_, ok := runtimeCapability[T](runtime)
		return ok
	})
	if !found {
		return zero, false
	}
	return runtimeCapability[T](runtime)
}

func runtimeCapabilities[T any](r *Runtime) *collectionlist.List[T] {
	if r == nil || r.runtimes == nil {
		return collectionlist.NewList[T]()
	}
	out := collectionlist.NewList[T]()
	r.runtimes.Range(func(_ int, runtime ecosystem.Runtime) bool {
		capability, ok := runtimeCapability[T](runtime)
		if ok {
			out.Add(capability)
		}
		return true
	})
	return out
}

func namedRuntimeCapability[T any](r *Runtime, name string, matcher runtimeNameMatcher, mode runtimeCapabilitySearchMode) (T, bool) {
	_, capability, ok := runtimeCapabilityByName[T](r, name, matcher, mode)
	return capability, ok
}

func runtimeCapabilityByName[T any](r *Runtime, name string, matcher runtimeNameMatcher, mode runtimeCapabilitySearchMode) (ecosystem.Runtime, T, bool) {
	var zero T
	if r == nil || r.runtimes == nil {
		return nil, zero, false
	}
	if matcher == nil {
		matcher = matchRuntimeNameExact
	}
	var matchedRuntime ecosystem.Runtime
	var match T
	var found bool
	r.runtimes.Range(func(_ int, runtime ecosystem.Runtime) bool {
		if runtime == nil || !matcher(runtime.Name(), name) {
			return true
		}
		matchedRuntime = runtime
		capability, ok := runtimeCapability[T](runtime)
		if ok {
			match = capability
			found = true
		}
		return continueRuntimeCapabilitySearch(mode, ok)
	})
	if !found {
		return matchedRuntime, zero, false
	}
	return matchedRuntime, match, true
}

func continueRuntimeCapabilitySearch(mode runtimeCapabilitySearchMode, foundCapability bool) bool {
	return mode != stopAfterRuntimeMatch && !foundCapability
}

func (r *Runtime) findRuntimeByName(name string, matcher runtimeNameMatcher) (ecosystem.Runtime, bool) {
	if r == nil || r.runtimes == nil {
		return nil, false
	}
	if matcher == nil {
		matcher = matchRuntimeNameExact
	}
	return collectionlist.FindList(r.runtimes, func(_ int, runtime ecosystem.Runtime) bool {
		return runtime != nil && matcher(runtime.Name(), name)
	})
}

// RuntimeCapabilitySearchMode controls whether a named lookup continues past a matching runtime.
type RuntimeCapabilitySearchMode = runtimeCapabilitySearchMode

const (
	// ContinueAfterRuntimeMatch allows lookup to find a capability on a later runtime with the same name.
	ContinueAfterRuntimeMatch RuntimeCapabilitySearchMode = continueAfterRuntimeMatch
	// StopAfterRuntimeMatch stops lookup at the first runtime whose name matches.
	StopAfterRuntimeMatch RuntimeCapabilitySearchMode = stopAfterRuntimeMatch
)

// RuntimeNameMatcher compares a runtime name with a lookup value.
type RuntimeNameMatcher = runtimeNameMatcher

// MatchRuntimeNameExact performs a case-sensitive runtime name match.
func MatchRuntimeNameExact(runtimeName, lookup string) bool {
	return matchRuntimeNameExact(runtimeName, lookup)
}

// MatchRuntimeNameFold performs a case-insensitive runtime name match.
func MatchRuntimeNameFold(runtimeName, lookup string) bool {
	return matchRuntimeNameFold(runtimeName, lookup)
}

// RuntimeCapabilities returns every runtime implementing T.
func RuntimeCapabilities[T any](runtime *Runtime) *collectionlist.List[T] {
	return runtimeCapabilities[T](runtime)
}

// NamedRuntimeCapability returns a named runtime capability using the requested search behavior.
func NamedRuntimeCapability[T any](
	runtime *Runtime,
	name string,
	matcher RuntimeNameMatcher,
	mode RuntimeCapabilitySearchMode,
) (T, bool) {
	return namedRuntimeCapability[T](runtime, name, matcher, mode)
}

// FindRuntimeByName returns the first runtime matching name.
func (r *Runtime) FindRuntimeByName(name string, matcher RuntimeNameMatcher) (ecosystem.Runtime, bool) {
	return r.findRuntimeByName(name, matcher)
}
