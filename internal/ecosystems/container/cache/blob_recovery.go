package cache

import (
	"context"
	"io"

	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/transfer"
)

func (p blobProxy) openRecoverableBlob(ctx context.Context, req BlobRequest, fullSize int64, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	reader, info, err := transfer.OpenObject(ctx, p.objects, transfer.ObjectRequest{
		Key: req.Digest, Digest: req.Digest, Size: fullSize, Options: opts,
	})
	if err != nil {
		return nil, nil, wrapError(err, "open recoverable blob object")
	}
	return reader, info, nil
}
