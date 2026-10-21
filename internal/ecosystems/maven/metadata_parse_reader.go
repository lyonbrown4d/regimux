package maven

import (
	"errors"
	"fmt"
	"io"
)

// The standard XML decoder may buffer a small amount across tokens. Bounding
// each refill interval prevents one huge start tag from allocating an entire
// input-sized attribute slice before the token-level counters can inspect it.
type metadataTokenInput struct {
	reader    io.Reader
	remaining int
}

func (input *metadataTokenInput) Read(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if input.remaining <= 0 {
		return 0, fmt.Errorf("%w: XML token exceeds read budget", errMavenMetadataStructureLimit)
	}
	written, err := input.reader.Read(data[:min(len(data), input.remaining)])
	input.remaining -= written
	if errors.Is(err, io.EOF) {
		return written, io.EOF
	}
	if err != nil {
		return written, fmt.Errorf("read Maven metadata token input: %w", err)
	}
	return written, nil
}
