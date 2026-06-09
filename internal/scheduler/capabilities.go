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
	var match T
	var found bool
	r.runtimes.Range(func(_ int, runtime ecosystem.Runtime) bool {
		capability, ok := runtimeCapability[T](runtime)
		if !ok {
			return true
		}
		match = capability
		found = true
		return false
	})
	if !found {
		return zero, false
	}
	return match, true
}

func runtimeCapabilities[T any](r *Runtime) *collectionlist.List[T] {
	matches := collectionlist.NewList[T]()
	if r == nil || r.runtimes == nil {
		return matches
	}
	r.runtimes.Range(func(_ int, runtime ecosystem.Runtime) bool {
		capability, ok := runtimeCapability[T](runtime)
		if ok {
			matches.Add(capability)
		}
		return true
	})
	return matches
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
	var found ecosystem.Runtime
	r.runtimes.Range(func(_ int, runtime ecosystem.Runtime) bool {
		if runtime == nil || !matcher(runtime.Name(), name) {
			return true
		}
		found = runtime
		return false
	})
	return found, found != nil
}
