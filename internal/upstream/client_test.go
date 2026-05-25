package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestClientGetManifestBearerChallenge(t *testing.T) {
	t.Parallel()

	var tokenRequests int
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequests++
		if got := r.URL.Path; got != "/token" {
			t.Errorf("token path = %q, want /token", got)
		}
		if got := r.URL.Query().Get("service"); got != "registry.test" {
			t.Errorf("token service = %q, want registry.test", got)
		}
		if got := r.URL.Query().Get("scope"); got != "repository:library/nginx:pull" {
			t.Errorf("token scope = %q, want repository:library/nginx:pull", got)
		}
		username, password, ok := r.BasicAuth()
		if !ok || username != "user" || password != "pass" {
			t.Errorf("token basic auth = %q/%q/%t, want user/pass/true", username, password, ok)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "challenge-token"})
	}))
	defer authServer.Close()

	var manifestRequests int
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		manifestRequests++
		if got := r.URL.Path; got != "/v2/library/nginx/manifests/latest" {
			t.Errorf("manifest path = %q, want /v2/library/nginx/manifests/latest", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.docker.distribution.manifest.v2+json" {
			t.Errorf("manifest accept = %q", got)
		}
		if manifestRequests == 1 {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="registry.test"`, authServer.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer challenge-token" {
			t.Errorf("manifest authorization = %q, want bearer challenge token", got)
		}
		body := `{"schemaVersion":2}`
		w.Header().Set("Docker-Content-Digest", "sha256:abc")
		w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		_, _ = io.WriteString(w, body)
	}))
	defer registryServer.Close()

	client := NewClient(map[string]Config{
		"hub": {
			Registry: registryServer.URL,
			Auth: AuthConfig{
				Type:     "dockerhub",
				Username: "user",
				Password: "pass",
			},
		},
	}, nil)

	resp, err := client.GetManifest(context.Background(), GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
		Accept:        "application/vnd.docker.distribution.manifest.v2+json",
	})
	if err != nil {
		t.Fatalf("GetManifest returned error: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read manifest body: %v", err)
	}
	if got := string(body); got != `{"schemaVersion":2}` {
		t.Fatalf("manifest body = %q", got)
	}
	if resp.Digest != "sha256:abc" {
		t.Fatalf("manifest digest = %q, want sha256:abc", resp.Digest)
	}
	if tokenRequests != 1 {
		t.Fatalf("token requests = %d, want 1", tokenRequests)
	}
	if manifestRequests != 2 {
		t.Fatalf("manifest requests = %d, want 2", manifestRequests)
	}
}

func TestClientGetBlobPreservesHeadRangeAndBearerToken(t *testing.T) {
	t.Parallel()

	const digest = "sha256:0123456789abcdef"
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodHead {
			t.Errorf("method = %q, want HEAD", got)
		}
		if got := r.URL.Path; got != "/v2/library/nginx/blobs/"+digest {
			t.Errorf("blob path = %q", got)
		}
		if got := r.Header.Get("Range"); got != "bytes=2-5" {
			t.Errorf("range = %q, want bytes=2-5", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer static-token" {
			t.Errorf("authorization = %q, want bearer static-token", got)
		}
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Content-Length", "4")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer registryServer.Close()

	client := NewClient(map[string]Config{
		"hub": {
			Registry: registryServer.URL,
			Auth:     AuthConfig{Type: "bearer", Token: "static-token"},
		},
	}, nil)

	resp, err := client.GetBlob(context.Background(), GetBlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        digest,
		Range:         &reference.HTTPRange{Start: 2, End: 5},
		Method:        http.MethodHead,
	})
	if err != nil {
		t.Fatalf("GetBlob returned error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("blob status = %d, want 206", resp.StatusCode)
	}
	if resp.Digest != digest {
		t.Fatalf("blob digest = %q, want %s", resp.Digest, digest)
	}
	if resp.Size != 4 {
		t.Fatalf("blob size = %d, want 4", resp.Size)
	}
}

func TestClientListTagsBuildsRequestURL(t *testing.T) {
	t.Parallel()

	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v2/library/nginx/tags/list" {
			t.Errorf("tags path = %q", got)
		}
		if got := r.URL.Query().Get("n"); got != "2" {
			t.Errorf("tags n = %q, want 2", got)
		}
		if got := r.URL.Query().Get("last"); got != "old" {
			t.Errorf("tags last = %q, want old", got)
		}
		_, _ = io.WriteString(w, `{"name":"library/nginx","tags":["latest"]}`)
	}))
	defer registryServer.Close()

	client := NewClient(map[string]Config{"hub": {Registry: registryServer.URL}}, nil)
	resp, err := client.ListTags(context.Background(), ListTagsRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		N:             "2",
		Last:          "old",
	})
	if err != nil {
		t.Fatalf("ListTags returned error: %v", err)
	}
	defer resp.Body.Close()
}

func TestRegistryURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		registry  string
		repo      string
		operation string
		value     string
		want      string
	}{
		{
			name:      "manifest reference",
			registry:  "https://registry.example.test/",
			repo:      "/library/nginx/",
			operation: "manifests",
			value:     "latest",
			want:      "https://registry.example.test/v2/library/nginx/manifests/latest",
		},
		{
			name:      "tags list",
			registry:  "https://registry.example.test",
			repo:      "library/nginx",
			operation: "tags/list",
			want:      "https://registry.example.test/v2/library/nginx/tags/list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := registryURL(tt.registry, tt.repo, tt.operation, tt.value); got != tt.want {
				t.Fatalf("registryURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMapStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		kind       string
		wantStatus int
		wantCode   distribution.ErrorCode
	}{
		{
			name:       "unauthorized",
			status:     http.StatusUnauthorized,
			kind:       "manifest",
			wantStatus: http.StatusUnauthorized,
			wantCode:   distribution.CodeUnauthorized,
		},
		{
			name:       "blob not found",
			status:     http.StatusNotFound,
			kind:       "blob",
			wantStatus: http.StatusNotFound,
			wantCode:   distribution.CodeBlobUnknown,
		},
		{
			name:       "manifest not found",
			status:     http.StatusNotFound,
			kind:       "manifest",
			wantStatus: http.StatusNotFound,
			wantCode:   distribution.CodeManifestUnknown,
		},
		{
			name:       "rate limited",
			status:     http.StatusTooManyRequests,
			kind:       "manifest",
			wantStatus: http.StatusTooManyRequests,
			wantCode:   distribution.CodeTooManyRequests,
		},
		{
			name:       "server error",
			status:     http.StatusBadGateway,
			kind:       "manifest",
			wantStatus: http.StatusBadGateway,
			wantCode:   distribution.CodeUpstreamError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := distribution.FromError(mapStatus(tt.status, tt.kind))
			if list.Status != tt.wantStatus {
				t.Fatalf("status = %d, want %d", list.Status, tt.wantStatus)
			}
			if len(list.Errors) != 1 || list.Errors[0].Code != tt.wantCode {
				t.Fatalf("code = %#v, want %s", list.Errors, tt.wantCode)
			}
		})
	}
}
