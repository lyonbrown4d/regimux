package cache

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
)

type blobFillAttempt struct {
	ctx  context.Context
	req  BlobRequest
	key  string
	fill *blobFill
}

type blobFillOwner struct {
	blobFillAttempt
	lease backend.Lease
}
