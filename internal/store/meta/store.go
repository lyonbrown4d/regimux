package meta

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("metadata not found")
	ErrInvalidKey   = errors.New("invalid metadata key")
	ErrInvalidValue = errors.New("invalid metadata value")
)

type Store interface {
	Close() error

	UpstreamByAlias(ctx context.Context, alias string) (*Upstream, error)
	RepositoryByName(ctx context.Context, upstreamID int64, name string) (*Repository, error)

	Manifest(ctx context.Context, key ManifestKey) (*ManifestRecord, bool, error)
	UpsertManifest(ctx context.Context, record ManifestRecord) (*ManifestRecord, error)
	DeleteManifest(ctx context.Context, key ManifestKey) error
	GetManifest(ctx context.Context, key string) (*ManifestRecord, bool, error)
	PutManifest(ctx context.Context, record ManifestRecord) error
	ListManifests(ctx context.Context) ([]ManifestRecord, error)

	Tag(ctx context.Context, key TagKey) (*TagRecord, bool, error)
	UpsertTag(ctx context.Context, record TagRecord) (*TagRecord, error)
	DeleteTag(ctx context.Context, key TagKey) error
	GetTag(ctx context.Context, key string) (*TagRecord, bool, error)
	PutTag(ctx context.Context, record TagRecord) error
	ListTags(ctx context.Context) ([]TagRecord, error)

	Pull(ctx context.Context, key PullKey) (*PullRecord, bool, error)
	RecordPull(ctx context.Context, key PullKey, at time.Time) (*PullRecord, error)
	RecordUpstreamPull(ctx context.Context, key PullKey, at time.Time) (*PullRecord, error)
	ListPulls(ctx context.Context) ([]PullRecord, error)

	Blob(ctx context.Context, key BlobKey) (*BlobRecord, bool, error)
	UpsertBlob(ctx context.Context, record BlobRecord) (*BlobRecord, error)
	DeleteBlob(ctx context.Context, key BlobKey) error
	GetBlob(ctx context.Context, digest string) (*BlobRecord, bool, error)
	PutBlob(ctx context.Context, record BlobRecord) error
	ListBlobs(ctx context.Context) ([]BlobRecord, error)

	RepoBlob(ctx context.Context, key RepoBlobKey) (*RepoBlobRecord, bool, error)
	UpsertRepoBlob(ctx context.Context, record RepoBlobRecord) (*RepoBlobRecord, error)
	DeleteRepoBlob(ctx context.Context, key RepoBlobKey) error
	ListRepoBlobs(ctx context.Context) ([]RepoBlobRecord, error)
}

type Upstream struct {
	ID               int64
	Alias            string
	RegistryURL      string
	DefaultNamespace string
	AuthType         string
	Enabled          bool
	TagTTL           time.Duration
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Repository struct {
	ID         int64
	UpstreamID int64
	Name       string
	CreatedAt  time.Time
	LastPullAt *time.Time
}

type ManifestKey struct {
	Alias      string `json:"alias"`
	Repository string `json:"repository"`
	Digest     string `json:"digest"`
}

type ManifestRecord struct {
	Key        string              `json:"key,omitempty"`
	Alias      string              `json:"alias"`
	Repository string              `json:"repository"`
	Reference  string              `json:"reference,omitempty"`
	AcceptKey  string              `json:"accept_key,omitempty"`
	Digest     string              `json:"digest"`
	MediaType  string              `json:"media_type"`
	Size       int64               `json:"size"`
	ObjectKey  string              `json:"object_key,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	ExpiresAt  time.Time           `json:"expires_at,omitzero"`
	CreatedAt  time.Time           `json:"created_at"`
	UpdatedAt  time.Time           `json:"updated_at"`
}

func (r ManifestRecord) Expired(now time.Time) bool {
	return !r.ExpiresAt.IsZero() && !now.Before(r.ExpiresAt)
}

type TagKey struct {
	Alias      string `json:"alias"`
	Repository string `json:"repository"`
	Reference  string `json:"reference"`
}

type TagRecord struct {
	Key        string    `json:"key,omitempty"`
	Alias      string    `json:"alias"`
	Repository string    `json:"repository"`
	Reference  string    `json:"reference"`
	Digest     string    `json:"digest"`
	ExpiresAt  time.Time `json:"expires_at,omitzero"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type PullKey struct {
	Alias      string `json:"alias"`
	Repository string `json:"repository"`
	Reference  string `json:"reference"`
}

type PullRecord struct {
	Key                string    `json:"key,omitempty"`
	Alias              string    `json:"alias"`
	Repository         string    `json:"repository"`
	Reference          string    `json:"reference"`
	Count              int64     `json:"count"`
	LastPullAt         time.Time `json:"last_pull_at,omitzero"`
	LastUpstreamPullAt time.Time `json:"last_upstream_pull_at,omitzero"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type BlobKey struct {
	Digest string `json:"digest"`
}

type BlobRecord struct {
	Digest       string    `json:"digest"`
	Size         int64     `json:"size"`
	MediaType    string    `json:"media_type"`
	ObjectKey    string    `json:"object_key,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastAccessAt time.Time `json:"last_access_at,omitzero"`
}

type RepoBlobKey struct {
	Alias      string `json:"alias"`
	Repository string `json:"repository"`
	Digest     string `json:"digest"`
}

type RepoBlobRecord struct {
	Key            string    `json:"key,omitempty"`
	Alias          string    `json:"alias"`
	Repository     string    `json:"repository"`
	Digest         string    `json:"digest"`
	SourceManifest string    `json:"source_manifest,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastAccessAt   time.Time `json:"last_access_at,omitzero"`
	LastVerifiedAt time.Time `json:"last_verified_at,omitzero"`
}

func (k ManifestKey) String() string {
	return k.Alias + "/" + k.Repository + "@" + k.Digest
}

func (k TagKey) String() string {
	return k.Alias + "/" + k.Repository + ":" + k.Reference
}

func (k PullKey) String() string {
	return k.Alias + "/" + k.Repository + ":" + k.Reference
}

func (k RepoBlobKey) String() string {
	return k.Alias + "/" + k.Repository + "@" + k.Digest
}
