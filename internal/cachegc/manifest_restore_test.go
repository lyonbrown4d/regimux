package cachegc_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/stretchr/testify/require"
)

func TestRestoreOCIManifest(t *testing.T) {
	valid := `{"schemaVersion":2,"manifests":[]}`
	for _, tc := range []struct {
		name, body string
		canceled   bool
		wantErr    error
	}{
		{name: "verified", body: valid},
		{name: "digest mismatch", body: `{"schemaVersion":2}`, wantErr: object.ErrDigestMismatch},
		{name: "canceled", body: valid, canceled: true, wantErr: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			objects, err := object.NewLocal(t.TempDir())
			require.NoError(t, err)
			service := cachegc.NewCleanupService(nil, objects)
			manifest := meta.ManifestRecord{Digest: digestForGraphBody([]byte(valid)), MediaType: distribution.MediaTypeOCIIndex}
			_, missingErr := service.OCIReferences(ctx, manifest)
			require.ErrorIs(t, missingErr, object.ErrNotFound)
			if tc.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err = service.RestoreOCIManifest(ctx, manifest, strings.NewReader(tc.body))
			require.ErrorIs(t, err, tc.wantErr)
			exists, err := objects.Exists(context.Background(), manifest.Digest)
			require.NoError(t, err)
			require.Equal(t, tc.wantErr == nil, exists)
			if tc.wantErr == nil {
				_, graphErr := service.OCIReferences(ctx, manifest)
				require.NoError(t, graphErr)
			}
		})
	}
}

func TestRestoreOCIManifestRejectsInvalidGraph(t *testing.T) {
	for _, body := range []string{`{`, `{"schemaVersion":2,"manifests":[{"digest":"invalid"}]}`} {
		objects, err := object.NewLocal(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		service := cachegc.NewCleanupService(nil, objects)
		manifest := meta.ManifestRecord{Digest: digestForGraphBody([]byte(body)), MediaType: distribution.MediaTypeOCIIndex}
		if restoreErr := service.RestoreOCIManifest(context.Background(), manifest, strings.NewReader(body)); restoreErr == nil {
			t.Fatal("accepted invalid graph")
		}
		exists, err := objects.Exists(context.Background(), manifest.Digest)
		if err != nil || exists {
			t.Fatalf("invalid graph persisted: %t %v", exists, err)
		}
	}
}

func TestRestoreOCIManifestRetainsSharedReferences(t *testing.T) {
	ctx := context.Background()
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	shared := "sha256:" + strings.Repeat("b", 64)
	body := `{"schemaVersion":2,"config":{"digest":"` + shared + `"},"layers":[{"digest":"` + shared + `"}]}`
	service := cachegc.NewCleanupService(nil, objects)
	manifest := meta.ManifestRecord{Digest: digestForGraphBody([]byte(body)), MediaType: distribution.MediaTypeOCIManifest}
	if restoreErr := service.RestoreOCIManifest(ctx, manifest, strings.NewReader(body)); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	refs, err := service.OCIReferences(ctx, manifest)
	if err != nil || len(refs) != 2 || refs[0] != shared || refs[1] != shared {
		t.Fatalf("lost shared references: %v %v", refs, err)
	}
}
