package pypi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
)

const (
	warmReadClients       = 4
	warmReadChunkBytes    = 64 << 10
	warmReadPause         = 20 * time.Millisecond
	warmReadCallerBudget  = 20 * time.Second
	warmReadWaitAllowance = 30 * time.Second
)

func TestPackageWarmCacheConcurrentReadersKeepIndependentProgress(t *testing.T) {
	fixture, seedService, upstream := newPackageTransferFixture(t, false)
	tail := packageTailFor(t, upstream.URL, "/packages/demo-1.0-py3-none-any.whl")
	seedAndVerifyWarmWheel(t, seedService, tail, fixture)

	observed := &observedPackageObjectStore{Store: fixture.objects}
	service := newObservedPackageService(upstream.URL, fixture.metadata, observed)
	ctx, cancel := context.WithTimeout(t.Context(), warmReadCallerBudget)
	defer cancel()
	results := readWarmPackageConcurrently(ctx, service, tail, warmReadClients)
	assertWarmPackageResults(t, results, fixture.digest)
	assertWarmObjectObservations(t, observed.snapshot())
}

func assertWarmPackageResults(t *testing.T, results []packageReadResult, digest string) {
	t.Helper()
	for index, result := range results {
		assertWarmPackageResult(t, index, result, digest)
	}
}

func assertWarmPackageResult(t *testing.T, index int, result packageReadResult, digest string) {
	t.Helper()
	if result.readErr != "" {
		t.Fatalf("reader %d failed after %d bytes (%s; caller context %v; max gap %s): %v", index, result.bytes, result.readErr, result.callerErr, result.maxGap, result.err)
	}
	if result.err != nil {
		t.Fatalf("reader %d failed: %v", index, result.err)
	}
	if result.bytes != progressWheelSize || result.sha256 != digest {
		t.Fatalf("reader %d bytes=%d sha256=%s, want %d/%s", index, result.bytes, result.sha256, progressWheelSize, digest)
	}
	if result.callerErr != nil {
		t.Fatalf("reader %d caller context ended: %v after %d bytes", index, result.callerErr, result.bytes)
	}
}

func assertWarmObjectObservations(t *testing.T, observations []*packageObjectReadObservation) {
	t.Helper()
	if len(observations) != warmReadClients {
		t.Fatalf("cache object opens=%d, want one per reader (%d)", len(observations), warmReadClients)
	}
	for index, observation := range observations {
		assertWarmObjectObservation(t, index, observation)
	}
}

func assertWarmObjectObservation(t *testing.T, index int, observation *packageObjectReadObservation) {
	t.Helper()
	if observation.openDuration >= pypiCacheOpenBudget {
		t.Fatalf("reader %d object open took %s, want below %s", index, observation.openDuration, pypiCacheOpenBudget)
	}
	if observation.readErr != "" {
		t.Fatalf("reader %d object source error=%s, source context=%s, bytes=%d", index, observation.readErr, observation.contextErr, observation.bytes)
	}
	if observation.maxGap >= 10*time.Second {
		t.Fatalf("reader %d source read gap=%s, want below 10s idle timeout", index, observation.maxGap)
	}
}

const pypiCacheOpenBudget = 2 * time.Second

type packageReadResult struct {
	bytes     int64
	sha256    string
	maxGap    time.Duration
	readErr   string
	callerErr error
	err       error
}

func readWarmPackageConcurrently(ctx context.Context, service *pypi.Service, tail string, clients int) []packageReadResult {
	start := make(chan struct{})
	results := make(chan packageReadResult, clients)
	for range clients {
		go func() {
			<-start
			results <- readWarmPackage(ctx, service, tail)
		}()
	}
	close(start)
	collected := make([]packageReadResult, 0, clients)
	for range clients {
		select {
		case result := <-results:
			collected = append(collected, result)
		case <-time.After(warmReadWaitAllowance):
			return append(collected, packageReadResult{err: errors.New("timed out collecting concurrent warm reads")})
		}
	}
	return collected
}

func readWarmPackage(ctx context.Context, service *pypi.Service, tail string) (result packageReadResult) {
	response, err := service.Get(ctx, pypi.Request{Alias: "pypi", Tail: tail, SkipPullRecord: true})
	if err != nil {
		result.err = fmt.Errorf("get warm wheel: %w", err)
		result.callerErr = ctx.Err()
		return result
	}
	if response.Cache != "hit" {
		result.err = fmt.Errorf("cache status=%q, want hit", response.Cache)
		result.err = errors.Join(result.err, closePackageBody(response.Body))
		return result
	}
	defer func() {
		result.err = errors.Join(result.err, closePackageBody(response.Body))
	}()
	return readWarmPackageBody(ctx, response.Body)
}

func readWarmPackageBody(ctx context.Context, body io.Reader) packageReadResult {
	result := packageReadResult{}
	hash := sha256.New()
	buffer := make([]byte, warmReadChunkBytes)
	var previousRead time.Time
	for {
		n, readErr := body.Read(buffer)
		if n > 0 {
			if err := recordWarmPackageProgress(hash, buffer[:n], &result, &previousRead); err != nil {
				result.err = err
				return result
			}
		}
		if readErr == io.EOF {
			result.sha256 = hex.EncodeToString(hash.Sum(nil))
			result.callerErr = ctx.Err()
			return result
		}
		if readErr != nil {
			return failedWarmPackageRead(ctx, result, readErr)
		}
	}
}

func recordWarmPackageProgress(hash io.Writer, chunk []byte, result *packageReadResult, previousRead *time.Time) error {
	if _, err := hash.Write(chunk); err != nil {
		return fmt.Errorf("hash warm wheel after %d bytes: %w", result.bytes, err)
	}
	result.bytes += int64(len(chunk))
	if !previousRead.IsZero() {
		gap := time.Since(*previousRead)
		if gap > result.maxGap {
			result.maxGap = gap
		}
	}
	*previousRead = time.Now()
	time.Sleep(warmReadPause)
	return nil
}

func failedWarmPackageRead(ctx context.Context, result packageReadResult, readErr error) packageReadResult {
	result.readErr = fmt.Sprintf("%T: %v", readErr, readErr)
	result.err = fmt.Errorf("read warm wheel after %d bytes: %w", result.bytes, readErr)
	result.callerErr = ctx.Err()
	return result
}

func seedAndVerifyWarmWheel(t *testing.T, service *pypi.Service, tail string, fixture *packageTransferFixture) {
	t.Helper()
	response, err := service.Get(t.Context(), pypi.Request{Alias: "pypi", Tail: tail, SkipPullRecord: true})
	requireNoError(t, "seed warm wheel", err)
	hash := sha256.New()
	bytesRead, err := io.Copy(hash, response.Body)
	requireNoError(t, "read complete seed wheel", err)
	closeBody(t, response.Body)
	if bytesRead != progressWheelSize || hex.EncodeToString(hash.Sum(nil)) != fixture.digest {
		t.Fatalf("seed bytes=%d sha256=%s, want %d/%s", bytesRead, hex.EncodeToString(hash.Sum(nil)), progressWheelSize, fixture.digest)
	}
	waitPackageTransferCache(t, fixture)
}
