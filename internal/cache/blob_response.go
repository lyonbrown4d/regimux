package cache

import (
	"net/http"
	"strconv"

	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func blobHeaders(info *object.Info) http.Header {
	headers := http.Header{}
	headers.Set("Content-Length", strconv.FormatInt(info.Size, 10))
	headers.Set("ETag", info.ETag)
	return headers
}

func blobReadOptions(req BlobRequest, fullSize int64, headers http.Header) (int, int64, object.GetOptions, error) {
	status := http.StatusOK
	size := fullSize
	opts := object.GetOptions{}
	if req.Range == nil {
		return status, size, opts, nil
	}

	resolved, err := req.Range.Resolve(fullSize)
	if err != nil {
		return 0, 0, object.GetOptions{}, distribution.ErrRangeInvalid.WithDetail(err.Error())
	}
	status = http.StatusPartialContent
	size = resolved.Length()
	headers.Set("Content-Length", strconv.FormatInt(size, 10))
	headers.Set("Content-Range", resolved.ContentRange(fullSize))
	opts.Range = req.Range
	return status, size, opts, nil
}
