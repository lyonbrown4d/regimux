package object_test

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/store/object"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestS3NativeClientHonorsCustomCABundle(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodHead, r.Method)
		w.Header().Set("Content-Length", "7")
		w.Header().Set("ETag", `"trusted-object"`)
	}))
	t.Cleanup(server.Close)
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	bundle := filepath.Join(t.TempDir(), "aws-ca.pem")
	require.NoError(t, os.WriteFile(bundle, certificate, 0o600))
	t.Setenv("AWS_CA_BUNDLE", bundle)
	store, err := object.NewS3(t.Context(), object.S3Options{
		Bucket: "bucket", Endpoint: server.URL, ForcePathStyle: true,
		AccessKeyID: "fixture", SecretAccessKey: "fixture",
	})
	require.NoError(t, err)
	info, err := store.Stat(t.Context(), ocidigest.FromString("payload").String())
	require.NoError(t, err, "the native AWS config loader must retain custom trust roots")
	require.Equal(t, int64(7), info.Size)
	require.Equal(t, "trusted-object", info.ETag)
}
