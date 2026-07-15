package upstreamhttp_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

func TestReadAllLimited(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		limit   int64
		want    string
		wantErr error
	}{
		{name: "below limit", body: "abc", limit: 4, want: "abc"},
		{name: "at limit", body: "abcd", limit: 4, want: "abcd"},
		{name: "over limit", body: "abcde", limit: 4, wantErr: upstreamhttp.ErrBodyTooLarge},
		{name: "invalid limit", body: "abc", limit: 0, wantErr: upstreamhttp.ErrInvalidLimit},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := upstreamhttp.ReadAllLimited(bytes.NewBufferString(test.body), test.limit)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ReadAllLimited() error = %v, want %v", err, test.wantErr)
			}
			if string(got) != test.want {
				t.Fatalf("ReadAllLimited() = %q, want %q", got, test.want)
			}
		})
	}
}

func FuzzReadAllLimited(f *testing.F) {
	const limit = int64(64)

	f.Add([]byte("metadata"))
	f.Add(bytes.Repeat([]byte("x"), int(limit)))
	f.Add(bytes.Repeat([]byte("x"), int(limit)+1))

	f.Fuzz(func(t *testing.T, input []byte) {
		body, err := upstreamhttp.ReadAllLimited(bytes.NewReader(input), limit)
		if int64(len(input)) > limit {
			if !errors.Is(err, upstreamhttp.ErrBodyTooLarge) {
				t.Fatalf("oversized input error = %v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("ReadAllLimited() error = %v", err)
		}
		if !bytes.Equal(body, input) {
			t.Fatal("ReadAllLimited() changed input")
		}
	})
}

func BenchmarkReadAllLimited(b *testing.B) {
	const size = 64 << 10
	payload := bytes.Repeat([]byte("x"), size)

	b.ReportAllocs()
	b.SetBytes(size)
	for b.Loop() {
		if _, err := upstreamhttp.ReadAllLimited(bytes.NewReader(payload), size); err != nil {
			b.Fatal(err)
		}
	}
}
