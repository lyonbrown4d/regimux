package container

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/artifactstream"
	"github.com/lyonbrown4d/regimux/internal/spool"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

// Admit before cache decoding and upstream parsing; release after spooling,
// so slow clients retain a file rather than one metadata-sized heap allocation.
func (e *RegistryEndpoint) metadataResponse(
	ctx context.Context,
	load func() (*registryOutput, func()),
) *registryOutput {
	out, after := e.loadAdmittedMetadataResponse(ctx, load)
	if after != nil {
		after()
	}
	return out
}

func (e *RegistryEndpoint) loadAdmittedMetadataResponse(
	ctx context.Context,
	load func() (*registryOutput, func()),
) (*registryOutput, func()) {
	if err := e.metadataGate.Acquire(ctx, 1); err != nil {
		return errorOutput(distribution.FromError(fmt.Errorf("admit registry metadata: %w", err))), nil
	}
	defer e.metadataGate.Release(1)
	return load()
}

func (e *RegistryEndpoint) streamMetadata(ctx context.Context, payload []byte) (httpx.ResponseStream, error) {
	body, err := spool.MaterializeReadCloser(io.NopCloser(bytes.NewReader(payload)), "regimux-oci-metadata-*")
	if err != nil {
		return nil, fmt.Errorf("spool registry metadata response: %w", err)
	}
	return artifactstream.Response(ctx, artifactstream.ResponseRequest{
		Status: http.StatusOK, Size: int64(len(payload)), Body: body, Logger: e.logger, Ecosystem: "oci",
	}), nil
}
