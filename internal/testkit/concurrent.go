// Package testkit provides shared test synchronization and assertion helpers.
package testkit

import (
	"sync"
	"testing"
	"time"
)

const concurrentTimeout = 2 * time.Second

type concurrentResult[T any] struct {
	value T
	err   error
}

// ConcurrentRun is a started group of synchronized calls.
type ConcurrentRun[T any] struct {
	count   int
	results <-chan concurrentResult[T]
}

// StartConcurrent starts count calls together and returns without waiting for their results.
func StartConcurrent[T any](count int, call func() (T, error)) *ConcurrentRun[T] {
	start := make(chan struct{})
	results := make(chan concurrentResult[T], count)
	var ready sync.WaitGroup
	ready.Add(count)
	for range count {
		go func() {
			ready.Done()
			<-start
			value, err := call()
			results <- concurrentResult[T]{value: value, err: err}
		}()
	}
	ready.Wait()
	close(start)
	return &ConcurrentRun[T]{count: count, results: results}
}

// Wait collects all call results or fails the test when a call errors or times out.
func (r *ConcurrentRun[T]) Wait(tb testing.TB) []T {
	tb.Helper()
	values := make([]T, 0, r.count)
	timer := time.NewTimer(concurrentTimeout)
	defer timer.Stop()
	for range r.count {
		select {
		case result := <-r.results:
			if result.err != nil {
				tb.Fatalf("concurrent call: %v", result.err)
			}
			values = append(values, result.value)
		case <-timer.C:
			tb.Fatal("concurrent calls did not return")
		}
	}
	return values
}

// WaitForSignal waits for a synchronization signal or fails the test.
func WaitForSignal(tb testing.TB, signal <-chan struct{}) {
	tb.Helper()
	select {
	case <-signal:
	case <-time.After(concurrentTimeout):
		tb.Fatal("synchronization signal was not received")
	}
}

// RequireOneMiss verifies that exactly one result is a miss and all others are hits.
func RequireOneMiss[T any, S comparable](tb testing.TB, values []T, miss, hit S, status func(T) S) {
	tb.Helper()
	counts := make(map[S]int, 2)
	for _, value := range values {
		counts[status(value)]++
	}
	if counts[miss] != 1 || counts[hit] != len(values)-1 {
		tb.Fatalf(
			"cache statuses: misses=%d hits=%d, want 1 miss and %d hits",
			counts[miss],
			counts[hit],
			len(values)-1,
		)
	}
}
