package meta

import "context"

// ConditionalGCDeleter lets cleanup remove only the exact metadata revisions it
// evaluated. Stores without conditional mutation support may use a read-check-delete fallback.
type ConditionalGCDeleter interface {
	DeleteBlobIfUnchanged(context.Context, BlobRecord) (bool, error)
	DeleteTagIfUnchanged(context.Context, TagRecord) (bool, error)
	DeleteManifestIfUnchanged(context.Context, ManifestRecord) (bool, error)
	DeleteRepoBlobIfUnchanged(context.Context, RepoBlobRecord) (bool, error)
}
