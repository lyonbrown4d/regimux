package pypi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
)

func TestUVLockUsesPyPIProxyAndRangeFetchesWheel(t *testing.T) {
	uvPath, err := exec.LookPath("uv")
	if err != nil {
		t.Skip("uv is not installed")
	}
	pythonPath, err := exec.LookPath("python")
	if err != nil {
		pythonPath, err = exec.LookPath("python3")
	}
	if err != nil {
		t.Skip("Python is not installed")
	}

	t.Run("primary package", func(t *testing.T) {
		runUVLockScenario(t, uvPath, pythonPath, false)
	})
	t.Run("mirror package after primary failure", func(t *testing.T) {
		runUVLockScenario(t, uvPath, pythonPath, true)
	})
}

func runUVLockScenario(t *testing.T, uvPath, pythonPath string, failPrimary bool) {
	t.Helper()
	wheel := buildAcceptanceWheel(t)
	wheelHash := sha256.Sum256(wheel)
	wheelDigest := hex.EncodeToString(wheelHash[:])
	const wheelName = "demo-1.0-py3-none-any.whl"
	counts := &acceptanceRequestCounts{}
	primary := httptest.NewServer(acceptancePrimaryHandler(t, wheel, wheelDigest, wheelName, failPrimary, counts))
	defer primary.Close()
	mirror := httptest.NewServer(acceptanceMirrorHandler(t, wheel, wheelDigest, wheelName, counts))
	defer mirror.Close()

	ctx := context.Background()
	_, metadata, objects := newTestServiceWithStores(ctx, t, primary.URL, nil)
	proxy := newAcceptanceProxy(t, pypi.NewService(pypi.ServiceDependencies{
		Config:   acceptanceUpstreamConfig(primary.URL, mirror.URL),
		Metadata: metadata,
		Objects:  objects,
	}))
	defer proxy.Close()

	projectDir := writeUVProject(t)
	runUVLockCommands(t, uvPath, pythonPath, proxy.URL+"/pypi/pypi/simple", projectDir)
	assertUVArtifactRequests(t, counts, failPrimary)
}

func TestLegacyPackageURLWithoutIdentityFallsBackUsingCachedIndex(t *testing.T) {
	wheel := buildAcceptanceWheel(t)
	wheelHash := sha256.Sum256(wheel)
	wheelDigest := hex.EncodeToString(wheelHash[:])
	const wheelName = "demo-1.0-py3-none-any.whl"
	counts := &acceptanceRequestCounts{}
	primary := httptest.NewServer(acceptancePrimaryHandler(t, wheel, wheelDigest, wheelName, true, counts))
	defer primary.Close()
	mirror := httptest.NewServer(acceptanceMirrorHandler(t, wheel, wheelDigest, wheelName, counts))
	defer mirror.Close()

	ctx := context.Background()
	_, metadata, objects := newTestServiceWithStores(ctx, t, primary.URL, nil)
	service := pypi.NewService(pypi.ServiceDependencies{
		Config:   acceptanceUpstreamConfig(primary.URL, mirror.URL),
		Metadata: metadata,
		Objects:  objects,
	})
	simple, err := service.Get(ctx, pypi.Request{Alias: "pypi", Tail: "simple/demo/"})
	if err != nil {
		t.Fatalf("seed simple index cache: %v", err)
	}
	_ = readResponse(t, simple)

	oldPackageURL := packageTailFor(t, primary.URL, "/packages/"+wheelName)
	resp, err := service.Get(ctx, pypi.Request{Alias: "pypi", Tail: oldPackageURL})
	if err != nil {
		t.Fatalf("fetch legacy package URL: %v", err)
	}
	assertLegacyMirrorResponse(t, resp, wheel)
	assertLegacyFallbackRequests(t, counts)
}
