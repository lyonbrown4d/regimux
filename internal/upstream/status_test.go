package upstream

import (
	"net/http"
	"testing"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestMapStatusNotFound(t *testing.T) {
	tests := []struct {
		name string
		kind string
		want distribution.ErrorCode
	}{
		{name: "blob", kind: operationBlob, want: distribution.CodeBlobUnknown},
		{name: "manifest", kind: operationManifest, want: distribution.CodeManifestUnknown},
		{name: "tags", kind: operationTags, want: distribution.CodeManifestUnknown},
		{name: "referrers", kind: operationReferrers, want: distribution.CodeManifestUnknown},
		{name: "ping", kind: operationPing, want: distribution.CodeManifestUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := mapStatus(http.StatusNotFound, tc.kind)
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

