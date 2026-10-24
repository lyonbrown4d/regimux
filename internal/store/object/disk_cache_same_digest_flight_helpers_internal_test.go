package object

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func startDiskCacheColdReads(requests []diskCacheColdRequest) []<-chan diskCacheColdReadResult {
	reads := make([]<-chan diskCacheColdReadResult, len(requests))
	for index, request := range requests {
		reads[index] = readDiskCacheColdRequest(request.reader)
	}
	return reads
}

func waitForDiskCacheColdRead(t *testing.T, result <-chan diskCacheColdReadResult) diskCacheColdReadResult {
	t.Helper()
	return receiveDiskCacheColdRead(t, result)
}

func assertDiskCacheColdFollowerBodies(
	t *testing.T,
	requests []diskCacheColdRequest,
	payload []byte,
	digest string,
) {
	t.Helper()
	reads := startDiskCacheColdReads(requests)
	for index, read := range reads {
		result := receiveDiskCacheColdRead(t, read)
		require.NoError(t, result.err, "reader %d", index)
		require.Equal(t, payload, result.body, "reader %d body", index)
		require.Equal(t, digest, digestForTest(result.body), "reader %d digest", index)
		require.Equal(t, digest, requests[index].info.Digest, "reader %d metadata digest", index)
		require.NoError(t, requests[index].reader.Close(), "close reader %d", index)
	}
}

func assertDiskCacheTemporaryDirectoryEmpty(t *testing.T, cache *DiskCacheStore) {
	t.Helper()
	entries, err := os.ReadDir(cache.tempDir)
	require.NoError(t, err, "read temporary directory")
	require.Empty(t, entries, "temporary files after abandoned fill")
}
