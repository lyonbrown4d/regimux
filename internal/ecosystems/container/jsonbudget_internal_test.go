package container

import (
	"errors"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
)

func TestManifestDescriptorDecodersCheckJSONBudgetBeforeTypedUnmarshal(t *testing.T) {
	nested := strings.Repeat("[", jsonbudget.DefaultLimits().MaxDepth+1) + "0" +
		strings.Repeat("]", jsonbudget.DefaultLimits().MaxDepth+1)
	body := []byte(`{"schemaVersion":2,"layers":[],"annotations":` + nested + `}`)
	manifest := &cache.CachedManifest{Body: body}

	if _, err := imageManifestBlobDescriptors(manifest); !errors.Is(err, jsonbudget.ErrLimit) {
		t.Fatalf("image manifest decode error = %v, want JSON budget limit", err)
	}
	if _, err := imageIndexManifestDescriptors(manifest); !errors.Is(err, jsonbudget.ErrLimit) {
		t.Fatalf("image index decode error = %v, want JSON budget limit", err)
	}
}
