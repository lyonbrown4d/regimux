package metadataxml_test

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven/metadataxml"
)

type metadataDocument struct {
	XMLName    xml.Name `xml:"metadata"`
	GroupID    string   `xml:"groupId,omitempty"`
	ArtifactID string   `xml:"artifactId,omitempty"`
}

func TestEncodeMatchesMarshalIndentAtExactLimit(t *testing.T) {
	document := metadataDocument{
		XMLName:    xml.Name{Local: "metadata"},
		GroupID:    "org.example",
		ArtifactID: "library",
	}
	encoded, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]byte(xml.Header), encoded...)

	var destination bytes.Buffer
	size, err := metadataxml.Encode(context.Background(), &destination, document, int64(len(expected)))
	if err != nil {
		t.Fatalf("encode at exact output limit: %v", err)
	}
	if size != int64(len(expected)) || !bytes.Equal(destination.Bytes(), expected) {
		t.Fatalf("encoded bytes differ from MarshalIndent: size=%d bytes=%q want=%q", size, destination.Bytes(), expected)
	}

	var limited bytes.Buffer
	_, err = metadataxml.Encode(context.Background(), &limited, document, int64(len(expected)-1))
	if !errors.Is(err, metadataxml.ErrOutputTooLarge) {
		t.Fatalf("one-byte-short output limit error = %v, want ErrOutputTooLarge", err)
	}
	if int64(limited.Len()) > int64(len(expected)-1) {
		t.Fatalf("limited writer emitted %d bytes past limit %d", limited.Len(), len(expected)-1)
	}
}

func TestEncodeStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var destination bytes.Buffer
	_, err := metadataxml.Encode(ctx, &destination, metadataDocument{}, 1024)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled serialization error = %v, want context.Canceled", err)
	}
	if destination.Len() != 0 {
		t.Fatalf("canceled serialization wrote %d bytes", destination.Len())
	}
}
