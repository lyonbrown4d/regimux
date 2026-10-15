package pypi_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/httpx"
	stdadapter "github.com/arcgolabs/httpx/adapter/std"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
)

type acceptanceWheelFile struct {
	name string
	body []byte
}

type acceptanceRequestCounts struct {
	primaryPackage atomic.Int32
	primaryRange   atomic.Int32
	mirrorIndex    atomic.Int32
	mirrorPackage  atomic.Int32
	mirrorRange    atomic.Int32
}

func acceptancePrimaryHandler(t *testing.T, wheel []byte, digest, filename string, failPackage bool, counts *acceptanceRequestCounts) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/simple/demo/":
			writeAcceptanceText(t, w, `<html><a href="%s/packages/%s#sha256=%s">demo</a></html>`, html.EscapeString(upstreamURL(r)), filename, digest)
		case "/packages/" + filename:
			counts.primaryPackage.Add(1)
			if r.Header.Get("Range") != "" {
				counts.primaryRange.Add(1)
			}
			if failPackage {
				http.Error(w, "primary artifact unavailable", http.StatusServiceUnavailable)
				return
			}
			http.ServeContent(w, r, filename, time.Unix(1, 0), bytes.NewReader(wheel))
		default:
			http.NotFound(w, r)
		}
	}
}

func acceptanceMirrorHandler(t *testing.T, wheel []byte, digest, filename string, counts *acceptanceRequestCounts) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/private/repository/simple/demo/":
			counts.mirrorIndex.Add(1)
			writeAcceptanceText(t, w, `<html><a href="%s/alternate/%s#sha256=%s">demo mirror</a></html>`, html.EscapeString(upstreamURL(r)), filename, digest)
		case "/alternate/" + filename:
			counts.mirrorPackage.Add(1)
			if r.Header.Get("Range") != "" {
				counts.mirrorRange.Add(1)
			}
			http.ServeContent(w, r, filename, time.Unix(1, 0), bytes.NewReader(wheel))
		default:
			http.NotFound(w, r)
		}
	}
}

func acceptanceUpstreamConfig(primaryURL, mirrorURL string) config.Config {
	return config.Config{PyPI: config.DependencyEcosystemConfig{
		"pypi": {Registry: primaryURL, Mirrors: []string{mirrorURL + "/private/repository/simple"}},
	}}
}

func newAcceptanceProxy(t *testing.T, service *pypi.Service) *httptest.Server {
	t.Helper()
	adapter := stdadapter.New(nil)
	server := httpx.New(httpx.WithAdapter(adapter))
	server.RegisterOnly(pypi.NewEndpoint(service))
	return httptest.NewServer(adapter.Router())
}

func writeUVProject(t *testing.T) string {
	t.Helper()
	projectDir := t.TempDir()
	project := "[project]\nname = \"regimux-uv-acceptance\"\nversion = \"0.1.0\"\nrequires-python = \">=3.8\"\ndependencies = [\"demo==1.0\"]\n"
	if err := os.WriteFile(filepath.Join(projectDir, "pyproject.toml"), []byte(project), 0o600); err != nil {
		t.Fatalf("write pyproject.toml: %v", err)
	}
	return projectDir
}

func runUVLockCommands(t *testing.T, uvPath, pythonPath, indexURL, projectDir string) {
	t.Helper()
	cacheDir := filepath.Join(t.TempDir(), "uv-cache")
	env := uvAcceptanceEnv(os.Environ(),
		"UV_CACHE_DIR="+cacheDir,
		"UV_PYTHON_DOWNLOADS=never",
		"UV_NO_CONFIG=true",
		"UV_INSECURE_HOST=127.0.0.1,localhost",
		"UV_INDEX=",
		"UV_INDEX_URL=",
		"UV_EXTRA_INDEX_URL=",
	)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if output, err := runUV(ctx, projectDir, env, uvPath, "lock", "--default-index", indexURL, "--python", pythonPath); err != nil {
		t.Fatalf("uv lock failed: %v\n%s", err, output)
	}
	if output, err := runUV(ctx, projectDir, env, uvPath, "lock", "--check", "--default-index", indexURL, "--python", pythonPath); err != nil {
		t.Fatalf("uv lock --check failed: %v\n%s", err, output)
	}
	lockPath := filepath.Join(projectDir, "uv.lock")
	info, err := os.Stat(lockPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("uv.lock was not created: info=%v error=%v", info, err)
	}
}

func assertUVArtifactRequests(t *testing.T, counts *acceptanceRequestCounts, failPrimary bool) {
	t.Helper()
	if failPrimary {
		if counts.primaryPackage.Load() == 0 || counts.mirrorIndex.Load() == 0 || counts.mirrorPackage.Load() == 0 {
			t.Fatalf("mirror fallback requests: primary=%d mirror-index=%d mirror-package=%d", counts.primaryPackage.Load(), counts.mirrorIndex.Load(), counts.mirrorPackage.Load())
		}
		if counts.mirrorRange.Load() == 0 {
			t.Fatal("uv did not issue a Range request to the fallback mirror artifact")
		}
		return
	}
	if counts.primaryPackage.Load() == 0 {
		t.Fatal("uv did not request the wheel through the configured PyPI proxy")
	}
	if counts.primaryRange.Load() == 0 {
		t.Fatal("uv did not issue a Range request to the primary upstream wheel")
	}
}

func assertLegacyMirrorResponse(t *testing.T, resp *pypi.Response, wheel []byte) {
	t.Helper()
	if resp.Status != http.StatusOK {
		t.Fatalf("legacy package status = %d, want %d", resp.Status, http.StatusOK)
	}
	if body := readResponse(t, resp); body != string(wheel) {
		t.Fatal("legacy package body differs from the mirror wheel")
	}
}

func assertLegacyFallbackRequests(t *testing.T, counts *acceptanceRequestCounts) {
	t.Helper()
	if counts.primaryPackage.Load() == 0 || counts.mirrorIndex.Load() == 0 || counts.mirrorPackage.Load() == 0 {
		t.Fatalf("legacy fallback requests: primary-package=%d mirror-index=%d mirror-package=%d", counts.primaryPackage.Load(), counts.mirrorIndex.Load(), counts.mirrorPackage.Load())
	}
}

func writeAcceptanceText(t *testing.T, writer io.Writer, format string, args ...any) {
	t.Helper()
	if _, err := fmt.Fprintf(writer, format, args...); err != nil {
		t.Errorf("write test response: %v", err)
	}
}

func buildAcceptanceWheel(t *testing.T) []byte {
	t.Helper()
	files := make([]acceptanceWheelFile, 0, 4)
	files = append(files,
		acceptanceWheelFile{name: "demo-1.0.dist-info/METADATA", body: []byte("Metadata-Version: 2.1\nName: demo\nVersion: 1.0\nRequires-Python: >=3.8\n\n")},
		acceptanceWheelFile{name: "demo-1.0.dist-info/WHEEL", body: []byte("Wheel-Version: 1.0\nGenerator: regimux-test\nRoot-Is-Purelib: true\nTag: py3-none-any\n\n")},
		acceptanceWheelFile{name: "demo/padding.bin", body: bytes.Repeat([]byte{0x5a}, 256<<10)},
	)

	var record strings.Builder
	for _, file := range files {
		digest := sha256.Sum256(file.body)
		writeAcceptanceText(t, &record, "%s,sha256=%s,%d\n", file.name, base64.RawURLEncoding.EncodeToString(digest[:]), len(file.body))
	}
	recordName := "demo-1.0.dist-info/RECORD"
	writeAcceptanceText(t, &record, "%s,,\n", recordName)
	files = append(files, acceptanceWheelFile{name: recordName, body: []byte(record.String())})

	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Store}
		header.SetMode(0o644)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create wheel entry %s: %v", file.name, err)
		}
		if _, err := entry.Write(file.body); err != nil {
			t.Fatalf("write wheel entry %s: %v", file.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close wheel archive: %v", err)
	}
	return archive.Bytes()
}

func upstreamURL(r *http.Request) string {
	return "http://" + r.Host
}

func runUV(ctx context.Context, projectDir string, env []string, uvPath string, args ...string) (string, error) {
	//nolint:gosec // uvPath is selected with exec.LookPath and this test supplies fixed arguments.
	command := exec.CommandContext(ctx, uvPath, args...)
	command.Dir = projectDir
	command.Env = env
	output, err := command.CombinedOutput()
	return string(output), err
}

func uvAcceptanceEnv(inherited []string, overrides ...string) []string {
	env := make([]string, 0, len(inherited)+len(overrides))
	for _, variable := range inherited {
		key, _, _ := strings.Cut(variable, "=")
		upperKey := strings.ToUpper(key)
		if strings.HasPrefix(upperKey, "UV_") && strings.Contains(upperKey, "INDEX") {
			continue
		}
		env = append(env, variable)
	}
	return append(env, overrides...)
}
