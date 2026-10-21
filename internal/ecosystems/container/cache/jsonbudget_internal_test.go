package cache

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
)

func TestManifestEnvelopeChecksJSONBudgetBeforeUnmarshal(t *testing.T) {
	body := deeplyNestedJSON()
	data := []byte(`{"record":{},"body":"e30=","ignored":` + string(body) + `}`)
	if _, err := manifestFromEnvelope(data); !errors.Is(err, jsonbudget.ErrLimit) {
		t.Fatalf("manifest envelope error = %v, want JSON budget limit", err)
	}
}

func TestProxyMetadataBodyChecksJSONBudget(t *testing.T) {
	_, err := readHTTPBody(io.NopCloser(bytes.NewReader(deeplyNestedJSON())), "manifest body")
	if !errors.Is(err, jsonbudget.ErrLimit) {
		t.Fatalf("proxy body error = %v, want JSON budget limit", err)
	}
}

func TestReferrersBodyChecksJSONBudgetBeforeMapDecode(t *testing.T) {
	err := validateReferrersBody(deeplyNestedJSON())
	if err == nil {
		t.Fatal("over-budget referrers body succeeded")
	}
	encoded, encodeErr := json.Marshal(err)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if !bytes.Contains(encoded, []byte(jsonbudget.ErrLimit.Error())) {
		t.Fatalf("referrers body error = %v, want JSON budget limit", err)
	}
}

func deeplyNestedJSON() []byte {
	depth := jsonbudget.DefaultLimits().MaxDepth + 1
	return []byte(`{"ignored":` + strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth) + `}`)
}
