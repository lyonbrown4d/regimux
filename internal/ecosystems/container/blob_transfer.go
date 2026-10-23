package container

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (e *RegistryEndpoint) blobTransfer(ctx context.Context, input *registryInput, route reference.Route, method string) *registryOutput {
	adjusted := *input
	if method == http.MethodHead {
		adjusted.Range = ""
	}
	if adjusted.Range != "" && !e.blobIfRangeMatches(ctx, route, adjusted.IfRange) {
		adjusted.Range = ""
	}
	out := e.blob(ctx, &adjusted, route, method)
	out.ETag = blobTransferETag(out.ETag, out.DockerContentDigest)
	if out.Status == http.StatusRequestedRangeNotSatisfiable && out.ContentRange == "" {
		metadata, err := e.blobTransferMetadata(ctx, route)
		if err == nil && metadata.Size >= 0 {
			out.ContentRange = fmt.Sprintf("bytes */%d", metadata.Size)
			out.AcceptRanges = distribution.RangeUnitBytes
		}
	}
	return out
}

func (e *RegistryEndpoint) blobIfRangeMatches(ctx context.Context, route reference.Route, value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	metadata, err := e.blobTransferMetadata(ctx, route)
	if err != nil {
		return false
	}
	headers := metadata.Headers.Clone()
	if headers == nil {
		headers = http.Header{}
	}
	headers.Set("ETag", blobTransferETag(headers.Get("ETag"), metadata.Digest))
	return transfer.MatchesIfRange(value, headers)
}

func (e *RegistryEndpoint) blobTransferMetadata(ctx context.Context, route reference.Route) (*cache.BlobReadResult, error) {
	result, err := e.blobs.Get(ctx, cache.BlobRequest{UpstreamAlias: route.Alias, Repo: route.Repo, Digest: route.Digest, Method: http.MethodHead})
	if err != nil {
		return nil, wrapError(err, "read blob validators")
	}
	if result.Reader != nil {
		if err := result.Reader.Close(); err != nil {
			return nil, wrapError(err, "close blob validator response")
		}
	}
	return result, nil
}

func blobTransferETag(value, digest string) string {
	if value == "" {
		value = digest
	}
	if value == "" || strings.HasPrefix(value, `"`) || strings.HasPrefix(value, `W/"`) {
		return value
	}
	return strconv.Quote(value)
}
