package cache

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/coalescer"
)

type blobFillAttempt struct {
	ctx  context.Context
	req  BlobRequest
	key  string
	fill *coalescer.Fill
}

type blobFillOwner struct {
	blobFillAttempt
	lease backend.Lease
}
