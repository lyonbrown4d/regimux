// Package reference_test verifies reference helpers through exported APIs.
package reference_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/reference"
)

func TestParseRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		header string
		want   reference.HTTPRange
	}{
		{"bytes=0-99", reference.HTTPRange{Start: 0, End: 99}},
		{"bytes=500-", reference.HTTPRange{Start: 500, End: -1}},
		{"bytes=-250", reference.HTTPRange{Start: -1, End: 250}},
	}
	for _, tt := range tests {
		got, err := reference.ParseRange(tt.header)
		if err != nil {
			t.Fatalf("ParseRange(%q) error = %v", tt.header, err)
		}
		if *got != tt.want {
			t.Fatalf("ParseRange(%q) = %+v, want %+v", tt.header, *got, tt.want)
		}
	}
}

func TestParseRangeEmpty(t *testing.T) {
	t.Parallel()

	got, err := reference.ParseRange("")
	if err != nil {
		t.Fatalf("ParseRange empty error = %v", err)
	}
	if got != nil {
		t.Fatalf("ParseRange empty = %+v, want nil", got)
	}
}

func TestParseRangeRejectsInvalid(t *testing.T) {
	t.Parallel()

	for _, header := range []string{"items=0-1", "bytes=0-1,2-3", "bytes=5-4", "bytes=-0"} {
		if _, err := reference.ParseRange(header); err == nil {
			t.Fatalf("ParseRange(%q) expected error", header)
		}
	}
}

func TestRangeResolve(t *testing.T) {
	t.Parallel()

	got, err := (reference.HTTPRange{Start: 500, End: -1}).Resolve(1000)
	if err != nil {
		t.Fatalf("Resolve open ended error = %v", err)
	}
	if *got != (reference.HTTPRange{Start: 500, End: 999}) {
		t.Fatalf("Resolve open ended = %+v", *got)
	}
	if got.Length() != 500 {
		t.Fatalf("Length() = %d, want 500", got.Length())
	}
	if got.ContentRange(1000) != "bytes 500-999/1000" {
		t.Fatalf("ContentRange() = %q", got.ContentRange(1000))
	}

	got, err = (reference.HTTPRange{Start: -1, End: 250}).Resolve(1000)
	if err != nil {
		t.Fatalf("Resolve suffix error = %v", err)
	}
	if *got != (reference.HTTPRange{Start: 750, End: 999}) {
		t.Fatalf("Resolve suffix = %+v", *got)
	}
}

func TestRangeString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		r    reference.HTTPRange
		want string
	}{
		{reference.HTTPRange{Start: 0, End: 99}, "bytes=0-99"},
		{reference.HTTPRange{Start: 500, End: -1}, "bytes=500-"},
		{reference.HTTPRange{Start: -1, End: 250}, "bytes=-250"},
	}
	for _, tt := range tests {
		if got := tt.r.String(); got != tt.want {
			t.Fatalf("HTTPRange.String() = %q, want %q", got, tt.want)
		}
	}
}
