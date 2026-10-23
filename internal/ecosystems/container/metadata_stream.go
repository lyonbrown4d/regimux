package container

import (
	"bytes"
	"context"
	"errors"
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
	load func() (*registryOutput, func() error),
) *registryOutput {
	out, after := e.loadAdmittedMetadataResponse(ctx, load)
	if after != nil {
		if err := after(); err != nil {
			if out != nil && out.cleanup != nil {
				err = errors.Join(err, out.cleanup())
			}
			return errorOutput(distribution.FromError(err))
		}
	}
	return out
}

func (e *RegistryEndpoint) loadAdmittedMetadataResponse(
	ctx context.Context,
	load func() (*registryOutput, func() error),
) (*registryOutput, func() error) {
	if err := e.metadataGate.Acquire(ctx, 1); err != nil {
		return errorOutput(distribution.FromError(fmt.Errorf("admit registry metadata: %w", err))), nil
	}
	defer e.metadataGate.Release(1)
	return load()
}

func (e *RegistryEndpoint) streamMetadata(ctx context.Context, payload []byte) (httpx.ResponseStream, error) {
	stream, _, err := e.streamMetadataWithCleanup(ctx, payload)
	return stream, err
}

func (e *RegistryEndpoint) streamMetadataWithCleanup(ctx context.Context, payload []byte) (httpx.ResponseStream, func() error, error) {
	body, err := spool.MaterializeReadCloser(io.NopCloser(bytes.NewReader(payload)), "regimux-oci-metadata-*")
	if err != nil {
		return nil, nil, fmt.Errorf("spool registry metadata response: %w", err)
	}
	stream := artifactstream.Response(ctx, artifactstream.ResponseRequest{
		Status: http.StatusOK, Size: int64(len(payload)), Body: body, Logger: e.logger, Ecosystem: "oci",
	})
	return stream, body.Close, nil
}
