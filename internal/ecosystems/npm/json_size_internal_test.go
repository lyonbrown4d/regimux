package npm

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestEncodedJSONSizeMatchesStandardLibraryEscaping(t *testing.T) {
	value := map[string]any{
		`key"\`:                   "quote\" slash\\ controls\b\f\n\r\t\x01 <>& \u2028\u2029 café 🙂",
		string([]byte{0xff, 'k'}): string([]byte{0xfe, 'v'}),
		"array":                   []any{nil, true, false, float64(1.25), float64(1e100), math.Copysign(0, -1), float64(1e-7)},
		"nil-array":               []any(nil),
		"nil-object":              map[string]any(nil),
	}
	want, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	got, err := encodedJSONSize(value)
	if err != nil {
		t.Fatalf("measure fixture: %v", err)
	}
	if got != int64(len(want)) {
		t.Fatalf("encoded JSON size = %d, json.Marshal length = %d, encoded=%s", got, len(want), want)
	}
}

func TestRewriteMetadataUsesExactOutputSizeLimit(t *testing.T) {
	body := []byte(`{"versions":{"1.0.0":{"dist":{"tarball":"https://registry.npmjs.org/demo/-/demo-1.0.0.tgz"}}}}`)
	proxyBase := "https://cache.example.test/a/longer/prefix"
	registryBase := "https://registry.npmjs.org"
	metadataURL := "https://registry.npmjs.org/demo"
	want, changed, err := rewriteMetadata(body, proxyBase, registryBase, metadataURL)
	if err != nil || !changed {
		t.Fatalf("rewrite fixture: changed=%t error=%v", changed, err)
	}
	got, changed, err := rewriteMetadataWithLimit(body, proxyBase, registryBase, metadataURL, int64(len(want)))
	if err != nil || !changed || len(got) != len(want) {
		t.Fatalf("rewrite at exact output size: length=%d changed=%t error=%v", len(got), changed, err)
	}
	if _, _, err := rewriteMetadataWithLimit(body, proxyBase, registryBase, metadataURL, int64(len(want)-1)); err == nil {
		t.Fatal("rewrite below exact output size succeeded")
	}
}

func TestRewriteMetadataStopsCumulativeTarballGrowth(t *testing.T) {
	const registryBase = "https://registry.npmjs.org"
	const metadataURL = "https://registry.npmjs.org/demo"
	proxyBase := "https://cache.example.test/" + strings.Repeat("x", 80)
	tarballs := []string{
		"https://registry.npmjs.org/demo/-/demo-1.0.0.tgz",
		"https://registry.npmjs.org/demo/-/demo-2.0.0.tgz",
	}
	growths := make([]int64, 0, len(tarballs))
	for _, tarball := range tarballs {
		length, ok := localTarballURLLength(tarball, proxyBase, registryBase, metadataURL)
		if !ok {
			t.Fatal("tarball fixture did not resolve")
		}
		growth := length - int64(len(tarball))
		if growth <= 0 {
			t.Fatalf("fixture tarball did not grow: %d", growth)
		}
		growths = append(growths, growth)
	}
	budget := max(growths[0], growths[1])
	body := []byte(`{"versions":{"1":{"dist":{"tarball":"https://registry.npmjs.org/demo/-/demo-1.0.0.tgz"}},"2":{"dist":{"tarball":"https://registry.npmjs.org/demo/-/demo-2.0.0.tgz"}}}}`)
	_, _, err := rewriteMetadataWithLimit(body, proxyBase, registryBase, metadataURL, budget)
	if err == nil || !strings.Contains(err.Error(), "exceeds body limit") {
		t.Fatalf("cumulative growth error = %v", err)
	}
}
