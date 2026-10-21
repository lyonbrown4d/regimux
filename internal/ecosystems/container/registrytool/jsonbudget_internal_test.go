package registrytool

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
)

func TestFetchManifestBodyChecksJSONBudget(t *testing.T) {
	depth := jsonbudget.DefaultLimits().MaxDepth + 1
	body := `{"ignored":` + strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth) + `}`
	_, err := readAndCloseManifest(io.NopCloser(strings.NewReader(body)))
	if !errors.Is(err, jsonbudget.ErrLimit) {
		t.Fatalf("registry manifest read error = %v, want JSON budget limit", err)
	}
}
