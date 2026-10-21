package prefetch

import (
	"errors"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
)

func TestPrefetchDescriptorDecoderChecksJSONBudget(t *testing.T) {
	depth := jsonbudget.DefaultLimits().MaxDepth + 1
	body := []byte(`{"manifests":[],"ignored":` + strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth) + `}`)
	_, err := imageIndexDescriptors(&cache.CachedManifest{Body: body})
	if !errors.Is(err, jsonbudget.ErrLimit) {
		t.Fatalf("prefetch index decode error = %v, want JSON budget limit", err)
	}
}
