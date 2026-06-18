package suggestion

import (
	"fmt"
	"slices"
	"testing"
)

func TestPrefixCandidateValuesFindsTagsBySharedPrefix(t *testing.T) {
	values := make([]string, 0, minPrefixSearchValues+3)
	for i := range minPrefixSearchValues {
		values = append(values, fmt.Sprintf("release-%02d", i))
	}
	values = append(values,
		"latest-pg17",
		"latest-pg18",
		"latest-pg18-oss",
	)

	got, ok := prefixCandidateValues("latest-18", values, 2)
	if !ok {
		t.Fatal("expected prefix candidates")
	}
	assertContains(t, got, "latest-pg18")
	assertContains(t, got, "latest-pg18-oss")
	assertNotContains(t, got, "release-00")
}

func TestPrefixCandidateValuesKeepsSmallSetsOnFullScan(t *testing.T) {
	got, ok := prefixCandidateValues("latest-18", []string{
		"latest-pg17",
		"latest-pg18",
	}, 2)
	if ok {
		t.Fatalf("ok = true, want false with small candidate set: %#v", got)
	}
}

func assertContains(t *testing.T, values []string, want string) {
	t.Helper()
	if slices.Contains(values, want) {
		return
	}
	t.Fatalf("values %#v do not contain %q", values, want)
}

func assertNotContains(t *testing.T, values []string, want string) {
	t.Helper()
	if slices.Contains(values, want) {
		t.Fatalf("values %#v unexpectedly contain %q", values, want)
	}
}
