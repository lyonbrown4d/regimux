package cache

import (
	"io"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/transfer"
)

func rangedBlobPassthrough(req BlobRequest, response *upstream.BlobResponse, reader io.ReadCloser) (*BlobReadResult, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	rangeHeader := ""
	if req.Range != nil {
		rangeHeader = req.Range.String()
	}
	selected, err := transfer.ApplyRange(method, rangeHeader, "", transfer.Response{
		Status: response.StatusCode, Headers: response.Headers, Body: reader, Size: response.Size,
	})
	if err != nil {
		return nil, wrapError(err, "select passthrough blob range")
	}
	if selected.Headers == nil {
		selected.Headers = http.Header{}
	}
	selected.Headers.Set("Accept-Ranges", "bytes")
	return &BlobReadResult{
		Reader: selected.Body, Digest: response.Digest, Size: selected.Size, Range: req.Range,
		Status: selected.Status, Headers: selected.Headers, Cache: CacheBypass,
	}, nil
}
