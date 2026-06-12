package container_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/suggestion"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestRegistryEndpointAppliesDefaultNamespace(t *testing.T) {
	manifests := &recordingManifestService{}
	endpoint := container.NewRegistryEndpointFromConfig(
		manifests,
		nil,
		nil,
		nil,
		nil,
		config.Config{
			Container: config.ContainerConfig{
				"hub": {DefaultNamespace: "library"},
			},
		},
	)
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/hello-world/manifests/latest")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
	if !bytes.Equal(body, []byte("{}")) {
		t.Fatalf("body = %q, want {}", body)
	}
	if manifests.Repo() != "library/hello-world" {
		t.Fatalf("repo = %q, want library/hello-world", manifests.Repo())
	}
}

func TestRegistryEndpointInvalidDigestReturnsDigestInvalid(t *testing.T) {
	baseURL := startAPIServer(t, container.NewRegistryEndpoint(nil, nil, nil, nil, nil))

	resp := httpGet(t, baseURL+"/v2/hub/library/alpine/blobs/not-a-digest")
	bodyBytes := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%q, want 400", resp.StatusCode, bodyBytes)
	}

	var body distribution.ErrorResponse
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("unmarshal error body: %v body=%q", err, bodyBytes)
	}
	if len(body.Errors) != 1 || body.Errors[0].Code != distribution.CodeDigestInvalid {
		t.Fatalf("error body = %#v, want DIGEST_INVALID", body.Errors)
	}
}

func TestRegistryEndpointManifestDeniedByPolicy(t *testing.T) {
	manifests := &recordingManifestService{}
	metadata := newTestMetadata(t)
	endpoint := container.NewRegistryEndpointFromOptions(manifests, nil, nil, nil, nil, container.RegistryEndpointOptions{
		Config: config.Config{
			Policy: config.PolicyConfig{
				Dependency: config.DependencyPolicyConfig{
					Block: []config.DependencyRuleConfig{
						{
							Ecosystem: "container",
							Alias:     "hub",
							Artifact:  "library/*",
						},
					},
				},
			},
		},
		Metadata: metadata,
	})
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/library/alpine/manifests/latest")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d body=%q, want 403", resp.StatusCode, body)
	}

	var bodyJSON distribution.ErrorResponse
	if err := json.Unmarshal(body, &bodyJSON); err != nil {
		t.Fatalf("unmarshal error response: %v body=%q", err, body)
	}
	if len(bodyJSON.Errors) != 1 || bodyJSON.Errors[0].Code != distribution.CodeDenied {
		t.Fatalf("unexpected error response: %#v", bodyJSON)
	}
	if got := manifests.Repo(); got != "" {
		t.Fatalf("manifest request should be blocked by policy, got repo = %q", got)
	}
	assertPolicyDeniedPull(t, metadata, meta.PullKey{
		Alias:      "hub",
		Repository: "library/alpine",
		Reference:  "latest",
	})
}

func TestRegistryEndpointSuggestsSimilarTagsForMissingManifest(t *testing.T) {
	manifests := &recordingManifestService{
		err: distribution.ErrManifestUnknown.WithDetail("missing manifest"),
	}
	suggestions := &recordingSuggestionService{
		result: suggestion.ManifestSuggestions{
			Tags: []string{"latest-pg18", "latest-pg18-oss"},
		},
	}
	endpoint := container.NewRegistryEndpointFromOptions(manifests, nil, nil, nil, nil, container.RegistryEndpointOptions{
		Suggestions: suggestions,
	})
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/timescale/timescaledb/manifests/latest-18")
	bodyBytes := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d body=%q, want 404", resp.StatusCode, bodyBytes)
	}

	var body manifestErrorResponse
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("unmarshal error body: %v body=%q", err, bodyBytes)
	}
	if len(body.Errors) != 1 || body.Errors[0].Code != distribution.CodeManifestUnknown {
		t.Fatalf("error body = %#v, want MANIFEST_UNKNOWN", body.Errors)
	}
	if !strings.Contains(body.Errors[0].Message, "latest-pg18") {
		t.Fatalf("message = %q, want tag suggestion", body.Errors[0].Message)
	}
	assertSuggestionRequest(t, suggestions.SuggestRequest(), suggestion.ManifestRequest{
		Alias:      "hub",
		Repository: "timescale/timescaledb",
		Reference:  "latest-18",
	})
	assertManifestSuggestions(t, body.Errors[0].Detail.Suggestions)
}

func TestRegistryEndpointManifestHitDoesNotRequestTagSuggestions(t *testing.T) {
	manifests := &recordingManifestService{}
	suggestions := &recordingSuggestionService{}
	endpoint := container.NewRegistryEndpointFromOptions(manifests, nil, nil, nil, nil, container.RegistryEndpointOptions{
		Suggestions: suggestions,
	})
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/timescale/timescaledb/manifests/latest-18")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
	if !bytes.Equal(body, []byte("{}")) {
		t.Fatalf("body = %q, want {}", body)
	}
	if got := suggestions.SuggestCalls(); got != 0 {
		t.Fatalf("suggest calls = %d, want 0", got)
	}
	if got := suggestions.ObserveCalls(); got != 1 {
		t.Fatalf("observe calls = %d, want 1", got)
	}
}

func TestRegistryEndpointHeadManifestMissHasNoSuggestionBody(t *testing.T) {
	manifests := &recordingManifestService{
		err: distribution.ErrManifestUnknown.WithDetail("missing manifest"),
	}
	suggestions := &recordingSuggestionService{
		result: suggestion.ManifestSuggestions{
			Tags: []string{"latest-pg18", "latest-pg18-oss"},
		},
	}
	endpoint := container.NewRegistryEndpointFromOptions(manifests, nil, nil, nil, nil, container.RegistryEndpointOptions{
		Suggestions: suggestions,
	})
	baseURL := startAPIServer(t, endpoint)

	resp := httpHead(t, baseURL+"/v2/hub/timescale/timescaledb/manifests/latest-18")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d body=%q, want 404", resp.StatusCode, body)
	}
	// Docker manifest resolution uses HEAD heavily; HTTP HEAD responses do not
	// expose the JSON error body, so Docker cannot display tag suggestions here.
	if len(body) != 0 {
		t.Fatalf("HEAD body = %q, want empty body", body)
	}
	if got := suggestions.SuggestCalls(); got != 1 {
		t.Fatalf("suggest calls = %d, want 1", got)
	}
}
