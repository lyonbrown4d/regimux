package cachegc_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestOCIReferencesRejectsMissingObjectsAndInvalidDescriptorDigests(t *testing.T) {
	ctx := context.Background()
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := cachegc.NewCleanupService(nil, objects)
	missingDigest := "sha256:" + strings.Repeat("a", 64)
	missing := meta.ManifestRecord{Digest: missingDigest, MediaType: distribution.MediaTypeOCIManifest}
	if _, err := service.OCIReferences(ctx, missing); err == nil {
		t.Fatal("expected missing OCI manifest object to fail closed")
	}

	body := `{"schemaVersion":2,"config":{"digest":"not-a-digest"},"layers":[]}`
	digest := digestForGraphBody([]byte(body))
	if _, err := objects.Put(ctx, digest, strings.NewReader(body), object.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	manifest := meta.ManifestRecord{Digest: digest, ObjectKey: digest, MediaType: distribution.MediaTypeOCIManifest}
	if _, err := service.OCIReferences(ctx, manifest); err == nil {
		t.Fatal("expected invalid descriptor digest to fail closed")
	}
}

func digestForGraphBody(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
