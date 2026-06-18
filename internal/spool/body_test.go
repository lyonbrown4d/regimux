package spool_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/spool"
	"github.com/stretchr/testify/require"
)

func TestMaterializeReadCloserCopiesAndRemovesTempBody(t *testing.T) {
	body, err := spool.MaterializeReadCloser(io.NopCloser(strings.NewReader("payload")), "regimux-spool-test-*")
	require.NoError(t, err)

	tmp, ok := body.(*spool.TempBody)
	require.True(t, ok)
	name := tmp.Name()

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "payload", string(data))
	require.NoError(t, body.Close())

	_, err = os.Stat(name)
	require.ErrorIs(t, err, os.ErrNotExist)
}
