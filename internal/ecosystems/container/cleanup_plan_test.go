package container_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	distribution "github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestPlanCacheCleanupGroupsOCIImageAndMergesTagsWithoutLayerPinning(t *testing.T) {
	ctx := context.Background()
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	layer := "sha256:" + strings.Repeat("a", 64)
	body, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     distribution.MediaTypeOCIManifest,
		"config":        map[string]any{"digest": "sha256:" + strings.Repeat("b", 64)},
		"layers":        []any{map[string]any{"digest": layer}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rootDigest := digestFor(body)
	mavenDigest := "sha256:" + strings.Repeat("c", 64)
	if _, putErr := objects.Put(ctx, rootDigest, strings.NewReader(string(body)), object.PutOptions{ContentType: distribution.MediaTypeOCIManifest}); putErr != nil {
		t.Fatal(putErr)
	}
	service := cache.NewCleanupService(nil, objects)
	runtime := container.NewRuntime(config.Config{Container: config.ContainerConfig{"registry": {}}}, nil, nil, nil, service)
	recent := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	inventory := ecosystem.CacheCleanupInventory{
		Manifests: []meta.ManifestRecord{
			{Alias: "registry", Repository: "team/app", Digest: rootDigest, MediaType: distribution.MediaTypeOCIManifest, ObjectKey: rootDigest, UpdatedAt: recent.Add(-time.Hour)},
			{Alias: "registry", Repository: "team/app", Digest: mavenDigest, AcceptKey: "maven", MediaType: "application/java-archive", UpdatedAt: recent},
		},
		Tags: []meta.TagRecord{
			{Alias: "registry", Repository: "team/app", Reference: "v1", Digest: rootDigest, UpdatedAt: recent.Add(-2 * time.Hour)},
			{Alias: "registry", Repository: "team/app", Reference: "stable", Digest: rootDigest, UpdatedAt: recent.Add(-3 * time.Hour)},
			{Alias: "registry", Repository: "team/app", Reference: "artifact", Digest: mavenDigest, UpdatedAt: recent},
		},
		RepoBlobs: []meta.RepoBlobRecord{{Alias: "registry", Repository: "team/app", Digest: layer, LastAccessAt: recent}},
	}
	groups, err := runtime.PlanCacheCleanup(ctx, inventory, ecosystem.CacheCleanupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want one image group: %#v", len(groups), groups)
	}
	group := groups[0]
	if len(group.Tags) != 2 {
		t.Fatalf("tags = %d, want both aliases for the same image", len(group.Tags))
	}
	if len(group.Digests) != 3 {
		t.Fatalf("digests = %v, want root, config, and layer", group.Digests)
	}
	if !group.LastAccessAt.Equal(recent.Add(-time.Hour)) {
		t.Fatalf("image activity = %s, want root metadata time %s; shared layer access must not pin this image", group.LastAccessAt, recent.Add(-time.Hour))
	}
}

func TestPlanCacheCleanupDoesNotEmitStandaloneGroupsForTaggedImageChildren(t *testing.T) {
	ctx := context.Background()
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rootDigest, childDigest := putIndexWithChild(ctx, t, objects, "sha256:"+strings.Repeat("b", 64))
	service := cache.NewCleanupService(nil, objects)
	runtime := container.NewRuntime(config.Config{Container: config.ContainerConfig{"registry": {}}}, nil, nil, nil, service)
	inventory := ecosystem.CacheCleanupInventory{
		Manifests: []meta.ManifestRecord{
			{Alias: "registry", Repository: "team/app", Digest: rootDigest, MediaType: distribution.MediaTypeOCIIndex, ObjectKey: rootDigest},
			{Alias: "registry", Repository: "team/app", Digest: childDigest, MediaType: distribution.MediaTypeOCIManifest, ObjectKey: childDigest},
		},
		Tags: []meta.TagRecord{{Alias: "registry", Repository: "team/app", Reference: "latest", Digest: rootDigest}},
	}

	groups, err := runtime.PlanCacheCleanup(ctx, inventory, ecosystem.CacheCleanupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want only tagged image group; child was emitted separately: %#v", len(groups), groups)
	}
	if len(groups[0].Manifests) != 2 || len(groups[0].Digests) != 3 {
		t.Fatalf("tagged group did not own the full image graph: %+v", groups[0])
	}
}

func TestPlanCacheCleanupGroupsUntaggedOCIImageWithItsChildren(t *testing.T) {
	ctx := context.Background()
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rootDigest, childDigest := putIndexWithChild(ctx, t, objects, "sha256:"+strings.Repeat("d", 64))
	service := cache.NewCleanupService(nil, objects)
	runtime := container.NewRuntime(config.Config{Container: config.ContainerConfig{"registry": {}}}, nil, nil, nil, service)
	inventory := ecosystem.CacheCleanupInventory{Manifests: []meta.ManifestRecord{
		{Alias: "registry", Repository: "team/app", Digest: rootDigest, MediaType: distribution.MediaTypeOCIIndex, ObjectKey: rootDigest},
		{Alias: "registry", Repository: "team/app", Digest: childDigest, MediaType: distribution.MediaTypeOCIManifest, ObjectKey: childDigest},
	}, RepoBlobs: []meta.RepoBlobRecord{{Alias: "registry", Repository: "team/app", Digest: childDigest, LastAccessAt: time.Now().UTC().Add(time.Hour)}}}

	groups, err := runtime.PlanCacheCleanup(ctx, inventory, ecosystem.CacheCleanupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Manifests) != 2 || len(groups[0].Digests) != 3 {
		t.Fatalf("untagged image group did not contain its full descriptor graph: %#v", groups)
	}
	if !groups[0].LastAccessAt.IsZero() {
		t.Fatalf("shared child access pinned the untagged image: %s", groups[0].LastAccessAt)
	}
}

func TestPlanCacheCleanupHonorsCancellation(t *testing.T) {
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := container.NewRuntime(config.Config{}, nil, nil, nil, cache.NewCleanupService(nil, objects))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtime.PlanCacheCleanup(ctx, ecosystem.CacheCleanupInventory{}, ecosystem.CacheCleanupOptions{}); err == nil {
		t.Fatal("expected canceled planner context to fail")
	}
}

func digestFor(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func putIndexWithChild(ctx context.Context, t *testing.T, objects object.Store, configDigest string) (string, string) {
	t.Helper()
	childBody, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     distribution.MediaTypeOCIManifest,
		"config":        map[string]any{"digest": configDigest},
		"layers":        []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	childDigest := digestFor(childBody)
	rootBody, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     distribution.MediaTypeOCIIndex,
		"manifests":     []any{map[string]any{"digest": childDigest}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rootDigest := digestFor(rootBody)
	for _, item := range []struct {
		digest string
		body   []byte
	}{
		{digest: childDigest, body: childBody},
		{digest: rootDigest, body: rootBody},
	} {
		if _, putErr := objects.Put(ctx, item.digest, strings.NewReader(string(item.body)), object.PutOptions{ContentType: distribution.MediaTypeOCIManifest}); putErr != nil {
			t.Fatal(putErr)
		}
	}
	return rootDigest, childDigest
}
