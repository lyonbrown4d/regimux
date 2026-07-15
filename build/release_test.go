package main

import "testing"

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantTag     string
		wantVersion string
	}{
		{
			name:        "tag",
			input:       "v1.2.9",
			wantTag:     "v1.2.9",
			wantVersion: "1.2.9",
		},
		{
			name:        "bare version",
			input:       "1.3.0-rc.1",
			wantTag:     "v1.3.0-rc.1",
			wantVersion: "1.3.0-rc.1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tag, version, err := normalizeVersion(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if tag != test.wantTag {
				t.Errorf("tag = %q, want %q", tag, test.wantTag)
			}
			if version != test.wantVersion {
				t.Errorf(
					"version = %q, want %q",
					version,
					test.wantVersion,
				)
			}
		})
	}
}

func TestNormalizeVersionRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	for _, input := range []string{" ", "release-1.2.9"} {
		if _, _, err := normalizeVersion(input); err == nil {
			t.Errorf("normalizeVersion(%q) error = nil", input)
		}
	}
}
