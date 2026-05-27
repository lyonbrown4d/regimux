package upstream_test

import (
	"net/http"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestMapStatusNotFound(t *testing.T) {
	tests := []struct {
		name string
		kind string
		want distribution.ErrorCode
	}{
		{name: "blob", kind: "blob", want: distribution.CodeBlobUnknown},
		{name: "manifest", kind: "manifest", want: distribution.CodeManifestUnknown},
		{name: "tags", kind: "tags", want: distribution.CodeManifestUnknown},
		{name: "referrers", kind: "referrers", want: distribution.CodeManifestUnknown},
		{name: "ping", kind: "ping", want: distribution.CodeManifestUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := upstream.MapStatus(http.StatusNotFound, tc.kind)
			list := distribution.FromError(err)
			if list == nil {
				t.Fatal("expected mapped distribution error")
			}
			if got := list.Errors[0].Code; got != tc.want {
				t.Fatalf("want code %s, got %s", tc.want, got)
			}
		})
	}
}
