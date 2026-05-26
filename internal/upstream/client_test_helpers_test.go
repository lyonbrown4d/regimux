package upstream_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/upstream"
)

func requireNoError(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
}

func newTestClient(configs map[string]upstream.Config) *upstream.Client {
	ordered := collectionmapping.NewOrderedMapWithCapacity[string, upstream.Config](len(configs))
	aliases := collectionlist.NewList(collectionmapping.NewMapFrom(configs).Keys()...).
		Sort(strings.Compare).
		Values()
	for _, alias := range aliases {
		ordered.Set(alias, configs[alias])
	}
	return upstream.NewClient(ordered, nil, nil)
}

func requireEqual[T comparable](t *testing.T, got, want T, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response JSON: %v", err)
	}
}

func writeString(t *testing.T, w http.ResponseWriter, value string) {
	t.Helper()
	if _, err := io.WriteString(w, value); err != nil {
		t.Fatalf("write response body: %v", err)
	}
}

func closeBody(t *testing.T, body io.Closer) {
	t.Helper()
	if err := body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}

func readAndClose(t *testing.T, body io.ReadCloser) string {
	t.Helper()
	data, err := io.ReadAll(body)
	requireNoError(t, err, "read response body")
	closeBody(t, body)
	return string(data)
}
