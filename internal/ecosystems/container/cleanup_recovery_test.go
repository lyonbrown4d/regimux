package container_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestCleanupRecoversMissingManifestByDigest(t *testing.T) {
	body := `{"schemaVersion":2,"manifests":[]}`
	digest := ocidigest.FromString(body).String()
	for _, tc := range []struct {
		name                    string
		dryRun, tagged, badBody bool
	}{
		{name: "tagged", tagged: true}, {name: "untagged"},
		{name: "dry run", tagged: true, dryRun: true}, {name: "wrong digest", tagged: true, badBody: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(cleanupRecoveryHandler(t, digest, body, tc.badBody, &requests))
			defer server.Close()
			configs := collectionmapping.NewOrderedMapWithCapacity[string, upstream.Config](1)
			configs.Set("registry", upstream.Config{Registry: server.URL})
			client := upstream.NewClient(upstream.ClientDependencies{Configs: configs})
			objects, err := object.NewLocal(t.TempDir())
			require.NoError(t, err)
			runtime := container.NewRuntime(config.Config{}, client, nil, nil, cache.NewCleanupService(nil, objects))
			healthyBody := `{"schemaVersion":2,"manifests":[],"healthy":true}`
			healthyDigest := ocidigest.FromString(healthyBody).String()
			_, putErr := objects.Put(context.Background(), healthyDigest, strings.NewReader(healthyBody), object.PutOptions{})
			require.NoError(t, putErr)
			inventory := ecosystem.CacheCleanupInventory{Manifests: []meta.ManifestRecord{
				{Alias: "registry", Repository: "image", Digest: digest, MediaType: distribution.MediaTypeOCIIndex},
				{Alias: "registry", Repository: "healthy", Digest: healthyDigest, MediaType: distribution.MediaTypeOCIIndex},
			}}
			if tc.tagged {
				inventory.Tags = []meta.TagRecord{{Alias: "registry", Repository: "image", Reference: "latest", Digest: digest}}
			}
			for range 2 {
				groups, planErr := runtime.PlanCacheCleanup(context.Background(), inventory, ecosystem.CacheCleanupOptions{DryRun: tc.dryRun})
				assertRecoveryPlan(t, groups, planErr, tc.dryRun || tc.badBody)
			}
			exists, err := objects.Exists(context.Background(), digest)
			require.NoError(t, err)
			require.Equal(t, !tc.dryRun && !tc.badBody, exists)
			assertRecoveryRequests(t, requests, tc.dryRun, tc.badBody)
		})
	}
}

func cleanupRecoveryHandler(t *testing.T, digest, body string, badBody bool, requests *int) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests++
		if !strings.HasSuffix(r.URL.Path, "/manifests/"+digest) {
			t.Errorf("mutable recovery request: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", distribution.MediaTypeOCIIndex)
		response := body
		if badBody {
			response = `{"schemaVersion":2,"manifests":[],"extra":true}`
		}
		if _, err := w.Write([]byte(response)); err != nil {
			t.Errorf("write manifest response: %v", err)
		}
	})
}

func assertRecoveryPlan(t *testing.T, groups []ecosystem.CacheCleanupGroup, err error, blocked bool) {
	t.Helper()
	if blocked {
		require.ErrorIs(t, err, object.ErrNotFound)
		require.Empty(t, groups)
		return
	}
	require.NoError(t, err)
	require.Len(t, groups, 2)
}

func assertRecoveryRequests(t *testing.T, requests int, dryRun, badBody bool) {
	t.Helper()
	if dryRun {
		require.Zero(t, requests)
		return
	}
	if !badBody {
		require.Equal(t, 1, requests, "repeated recovery")
	}
}
