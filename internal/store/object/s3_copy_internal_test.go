package object

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestS3UploadCopyStopsAtSizeLimit(t *testing.T) {
	source := strings.NewReader("0123456789-unread-data")
	var destination bytes.Buffer
	err := copyS3Upload(&destination, source, 10)
	if err == nil || !strings.Contains(err.Error(), "exceeds 10000 parts") {
		t.Fatalf("oversized copy error=%v", err)
	}
	if destination.Len() != 11 || source.Len() != len("unread-data") {
		t.Fatalf("copy consumed beyond limit+1: written=%d remaining=%d", destination.Len(), source.Len())
	}
	if err := copyS3Upload(io.Discard, strings.NewReader("0123456789"), 10); err != nil {
		t.Fatalf("exactly at size limit: %v", err)
	}
}
