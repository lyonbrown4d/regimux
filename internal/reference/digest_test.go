package reference

import "testing"

func TestNormalizeDigest(t *testing.T) {
	t.Parallel()

	raw := "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	got, err := NormalizeDigest(raw)
	if err != nil {
		t.Fatalf("NormalizeDigest() error = %v", err)
	}
	want := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if got != want {
		t.Fatalf("NormalizeDigest() = %q, want %q", got, want)
	}
}

func TestNormalizeDigestRejectsInvalid(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"sha256",
		"sha256:not-hex",
		"sha256:abcd",
		"md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/../x",
	}
	for _, tt := range tests {
		if _, err := NormalizeDigest(tt); err == nil {
			t.Fatalf("NormalizeDigest(%q) expected error", tt)
		}
	}
}
