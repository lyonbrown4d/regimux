package transfer_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/transfer"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestOpenObjectTreatsZeroAsDeclaredLength(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		body      string
		size      int64
		wantError bool
	}{
		{name: "zero rejects nonempty object", body: "x", size: 0, wantError: true},
		{name: "zero accepts empty object", body: "", size: 0},
		{name: "negative permits unknown size", body: "xyz", size: -1},
		{name: "positive accepts exact size", body: "xyz", size: 3},
		{name: "positive rejects wrong size", body: "xyz", size: 2, wantError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			digest := ocidigest.SHA256.FromString(testCase.body).String()
			store := &declaredSizeObjectStore{body: testCase.body, digest: digest}
			reader, info, err := transfer.OpenObject(t.Context(), store, transfer.ObjectRequest{
				Key: digest, Digest: digest, Size: testCase.size,
			})
			if reader != nil {
				t.Cleanup(func() { require.NoError(t, reader.Close()) })
			}
			if testCase.wantError {
				require.Error(t, err, "explicit object size was not validated")
				require.Nil(t, reader)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, info)
			actual, readErr := io.ReadAll(reader)
			require.NoError(t, readErr)
			require.Equal(t, testCase.body, string(actual))
		})
	}
}

type declaredSizeObjectStore struct {
	object.Store
	body   string
	digest string
}

func (s *declaredSizeObjectStore) Get(_ context.Context, _ string, _ object.GetOptions) (io.ReadCloser, *object.Info, error) {
	return io.NopCloser(strings.NewReader(s.body)), &object.Info{Digest: s.digest, Size: int64(len(s.body))}, nil
}
