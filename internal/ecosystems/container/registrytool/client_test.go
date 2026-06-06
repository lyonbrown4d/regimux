package registrytool_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/registrytool"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestORASRepositoryUsesNormalizedRegistry(t *testing.T) {
	client := registrytool.NewClient()

	repo, err := client.ORASRepository(registrytool.RepositoryRef{
		Registry:   "http://registry.test:5000/",
		Repository: "library/node",
		PageSize:   50,
	})

	require.NoError(t, err)
	require.True(t, repo.PlainHTTP)
	require.Equal(t, 50, repo.TagListPageSize)
	require.Equal(t, "registry.test:5000", repo.Reference.Registry)
	require.Equal(t, "library/node", repo.Reference.Repository)
}

func TestClientHeadAndFetchManifestUseORASRepository(t *testing.T) {
	body := []byte(`{"schemaVersion":2}`)
	digest := ocidigest.FromBytes(body).String()
	requests := []string{}
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.URL.Path != "/v2/library/node/manifests/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
		w.Header().Set(distribution.HeaderDockerContentDigest, digest)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			_, err := w.Write(body)
			require.NoError(t, err)
		}
	}))
	defer registryServer.Close()

	client := registrytool.NewClient()
	ref := registrytool.Reference{
		Registry:   registryServer.URL,
		Repository: "library/node",
		Reference:  "latest",
	}

	desc, err := client.Head(context.Background(), ref)
	require.NoError(t, err)
	require.Equal(t, distribution.MediaTypeDockerManifest, desc.MediaType)
	require.Equal(t, digest, desc.Digest)
	require.Equal(t, int64(len(body)), desc.Size)

	manifest, err := client.FetchManifest(context.Background(), ref)
	require.NoError(t, err)
	require.Equal(t, desc, manifest.Descriptor)
	require.Equal(t, body, manifest.Content)
	require.Contains(t, requests, "HEAD /v2/library/node/manifests/latest")
	require.Contains(t, requests, "GET /v2/library/node/manifests/latest")
}
