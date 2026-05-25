package reference

import "testing"

func TestNormalizeAccept(t *testing.T) {
	t.Parallel()

	got := NormalizeAccept(" application/vnd.oci.image.manifest.v1+json ; q=1.0 , Application/Vnd.Docker.Distribution.Manifest.V2+JSON; charset=utf-8 ")
	want := "application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.v2+json;charset=utf-8"
	if got != want {
		t.Fatalf("NormalizeAccept() = %q, want %q", got, want)
	}
}

func TestNormalizeAcceptPreservesOrder(t *testing.T) {
	t.Parallel()

	first := NormalizeAccept("application/a,application/b")
	second := NormalizeAccept("application/b,application/a")
	if first == second {
		t.Fatalf("NormalizeAccept should preserve media-range order, got %q", first)
	}
}

func TestAcceptKey(t *testing.T) {
	t.Parallel()

	a := AcceptKey("application/json ; q=1")
	b := AcceptKey("application/json")
	if a != b {
		t.Fatalf("AcceptKey should use normalized header, got %q and %q", a, b)
	}
}
