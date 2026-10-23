package upstream

import (
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/lo"
)

func blobResponseFromUpstream(resp upstreamResponse, expectedDigest string) (*BlobResponse, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, closeBodyWithError(resp.Body, mapStatus(resp.StatusCode, operationBlob))
	}
	actualDigest := resp.Header.Get(distribution.HeaderDockerContentDigest)
	if err := validateUpstreamContentDigest(expectedDigest, actualDigest); err != nil {
		return nil, closeBodyWithError(resp.Body, err)
	}
	return &BlobResponse{
		Body:       resp.Body,
		Digest:     lo.CoalesceOrEmpty(actualDigest, expectedDigest),
		Size:       contentLength(resp.Header),
		StatusCode: resp.StatusCode,
		Headers:    resp.Header.Clone(),
	}, nil
}
