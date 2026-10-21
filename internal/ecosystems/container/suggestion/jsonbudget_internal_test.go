package suggestion

import (
	"errors"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
)

func TestSuggestionTagDecodersCheckJSONBudget(t *testing.T) {
	depth := jsonbudget.DefaultLimits().MaxDepth + 1
	body := []byte(`{"tags":[],"ignored":` + strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth) + `}`)
	if _, err := decodeTagList(body); !errors.Is(err, jsonbudget.ErrLimit) {
		t.Fatalf("tag list decode error = %v, want JSON budget limit", err)
	}
	if _, err := tagIndexFromCache(body); !errors.Is(err, jsonbudget.ErrLimit) {
		t.Fatalf("tag cache decode error = %v, want JSON budget limit", err)
	}
}
