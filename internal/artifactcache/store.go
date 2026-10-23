package artifactcache

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/coalescer"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/samber/oops"
)

func New(deps Dependencies) *Store {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	fills := deps.Fills
	if fills == nil {
		fills = NewFillTracker()
	}
	return &Store{
		metadata:     deps.Metadata,
		objects:      deps.Objects,
		fills:        fills,
		logger:       logger.With("component", "artifact-cache"),
		now:          now,
		accesses:     coalescer.NewTracker(),
		submitAccess: deps.SubmitAccess,
	}
}

func (s *Store) Metadata() MetadataStore {
	if s == nil {
		return nil
	}
	return s.metadata
}

func (s *Store) Objects() object.Store {
	if s == nil {
		return nil
	}
	return s.objects
}

func (s *Store) FillTracker() *FillTracker {
	if s == nil {
		return nil
	}
	return s.fills
}

func (s *Store) Get(ctx context.Context, key Key) (Entry, bool, error) {
	snapshot, err := s.readCachedMetadata(ctx, key)
	if err != nil {
		return Entry{}, false, err
	}
	if !snapshot.found {
		return Entry{}, false, nil
	}
	entry, ok, err := s.open(ctx, snapshot.manifest)
	if err != nil || !ok {
		return Entry{}, ok, err
	}
	now := s.now()
	entry.Expired = expiredAt(snapshot.tag.ExpiresAt, now) || snapshot.manifest.Expired(now)
	return entry, true, nil
}

// LookupManifest resolves and touches cached metadata without opening the object body.
// Each caller owns its returned record and header values.
func LookupManifest(ctx context.Context, s *Store, key Key) (*meta.ManifestRecord, bool, error) {
	snapshot, err := s.readCachedMetadata(ctx, key)
	if err != nil || !snapshot.found {
		return nil, false, err
	}
	manifest := cloneManifestHeaders(snapshot.manifest)
	return &manifest, true, nil
}

func (s *Store) readCachedMetadata(ctx context.Context, key Key) (readSnapshot, error) {
	if s == nil || s.metadata == nil || s.objects == nil {
		return readSnapshot{}, nil
	}
	snapshot, err, _ := s.reads.DoContext(ctx, key, func(sharedCtx context.Context) (readSnapshot, error) {
		return s.readAndTouch(sharedCtx, key)
	})
	if err != nil {
		return readSnapshot{}, wrapError(err, "coalesce artifact cache read")
	}
	return snapshot, nil
}

type readSnapshot struct {
	tag      meta.TagRecord
	manifest meta.ManifestRecord
	found    bool
}

func (s *Store) readAndTouch(ctx context.Context, key Key) (readSnapshot, error) {
	tag, manifest, ok, err := s.lookup(ctx, key)
	if err != nil || !ok {
		return readSnapshot{}, err
	}
	s.touchAccess(ctx, *manifest, s.now())
	return readSnapshot{
		tag:      *tag,
		manifest: cloneManifestHeaders(*manifest),
		found:    true,
	}, nil
}

func cloneManifestHeaders(manifest meta.ManifestRecord) meta.ManifestRecord {
	manifest.Headers = http.Header(manifest.Headers).Clone()
	return manifest
}

func (s *Store) lookup(ctx context.Context, key Key) (*meta.TagRecord, *meta.ManifestRecord, bool, error) {
	tag, ok, err := s.metadata.Tag(ctx, meta.TagKey{
		Alias:      key.Alias,
		Repository: key.Repository,
		Reference:  key.Reference,
	})
	if err != nil {
		return nil, nil, false, wrapError(err, "lookup artifact cache tag")
	}
	if !ok {
		return nil, nil, false, nil
	}
	manifest, ok, err := s.metadata.Manifest(ctx, meta.ManifestKey{
		Alias:      key.Alias,
		Repository: key.Repository,
		Digest:     tag.Digest,
	})
	if err != nil {
		return nil, nil, false, wrapError(err, "lookup artifact cache manifest")
	}
	if !ok {
		return nil, nil, false, nil
	}
	return tag, manifest, true, nil
}

func (s *Store) open(ctx context.Context, manifest meta.ManifestRecord) (Entry, bool, error) {
	objectKey := manifest.ObjectKey
	if objectKey == "" {
		objectKey = manifest.Digest
	}
	reader, info, err := transfer.OpenObject(ctx, s.objects, transfer.ObjectRequest{
		Key: objectKey, Digest: manifest.Digest, Size: manifest.Size,
	})
	if errors.Is(err, object.ErrNotFound) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, wrapError(err, "open cached artifact object")
	}
	size := manifest.Size
	if size < 0 && info != nil {
		size = info.Size
	}
	return Entry{
		Digest:  manifest.Digest,
		Size:    size,
		Headers: http.Header(manifest.Headers).Clone(),
		Body:    reader,
	}, true, nil
}

func expiredAt(expiresAt, now time.Time) bool {
	return !expiresAt.IsZero() && !now.Before(expiresAt)
}

func wrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return oops.In("artifact-cache").Wrapf(err, "%s", message)
}
