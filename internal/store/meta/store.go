package meta

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/reference"
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

	Tag(ctx context.Context, key TagKey) (*TagRecord, bool, error)
	UpsertTag(ctx context.Context, record TagRecord) (*TagRecord, error)
	DeleteTag(ctx context.Context, key TagKey) error
	GetTag(ctx context.Context, key string) (*TagRecord, bool, error)
	PutTag(ctx context.Context, record TagRecord) error

	Blob(ctx context.Context, key BlobKey) (*BlobRecord, bool, error)
	UpsertBlob(ctx context.Context, record BlobRecord) (*BlobRecord, error)
	DeleteBlob(ctx context.Context, key BlobKey) error
	GetBlob(ctx context.Context, digest string) (*BlobRecord, bool, error)
	PutBlob(ctx context.Context, record BlobRecord) error

	RepoBlob(ctx context.Context, key RepoBlobKey) (*RepoBlobRecord, bool, error)
	UpsertRepoBlob(ctx context.Context, record RepoBlobRecord) (*RepoBlobRecord, error)
	DeleteRepoBlob(ctx context.Context, key RepoBlobKey) error
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
	ExpiresAt  time.Time           `json:"expires_at,omitempty"`
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
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
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
	LastAccessAt time.Time `json:"last_access_at,omitempty"`
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
	LastVerifiedAt time.Time `json:"last_verified_at,omitempty"`
}

func normalizeManifestRecord(record ManifestRecord) (ManifestKey, ManifestRecord, error) {
	key, err := normalizeManifestKey(ManifestKey{
		Alias:      record.Alias,
		Repository: record.Repository,
		Digest:     record.Digest,
	})
	if err != nil {
		return ManifestKey{}, ManifestRecord{}, err
	}
	if record.Size < 0 {
		return ManifestKey{}, ManifestRecord{}, fmt.Errorf("%w: manifest size cannot be negative", ErrInvalidValue)
	}
	record.Alias = key.Alias
	record.Repository = key.Repository
	record.Digest = key.Digest
	record.Key = key.String()
	record.Headers = cloneHeaders(record.Headers)
	return key, record, nil
}

func normalizeManifestKey(key ManifestKey) (ManifestKey, error) {
	alias, err := required("alias", key.Alias)
	if err != nil {
		return ManifestKey{}, err
	}
	repo, err := required("repository", key.Repository)
	if err != nil {
		return ManifestKey{}, err
	}
	digest, err := normalizeDigest(key.Digest)
	if err != nil {
		return ManifestKey{}, err
	}
	return ManifestKey{Alias: alias, Repository: repo, Digest: digest}, nil
}

func (k ManifestKey) String() string {
	return k.Alias + "/" + k.Repository + "@" + k.Digest
}

func normalizeTagRecord(record TagRecord) (TagKey, TagRecord, error) {
	key, err := normalizeTagKey(TagKey{
		Alias:      record.Alias,
		Repository: record.Repository,
		Reference:  record.Reference,
	})
	if err != nil {
		return TagKey{}, TagRecord{}, err
	}
	digest, err := normalizeDigest(record.Digest)
	if err != nil {
		return TagKey{}, TagRecord{}, err
	}
	record.Alias = key.Alias
	record.Repository = key.Repository
	record.Reference = key.Reference
	record.Digest = digest
	record.Key = key.String()
	return key, record, nil
}

func normalizeTagKey(key TagKey) (TagKey, error) {
	alias, err := required("alias", key.Alias)
	if err != nil {
		return TagKey{}, err
	}
	repo, err := required("repository", key.Repository)
	if err != nil {
		return TagKey{}, err
	}
	ref, err := required("reference", key.Reference)
	if err != nil {
		return TagKey{}, err
	}
	return TagKey{Alias: alias, Repository: repo, Reference: ref}, nil
}

func (k TagKey) String() string {
	return k.Alias + "/" + k.Repository + ":" + k.Reference
}

func normalizeBlobRecord(record BlobRecord) (BlobKey, BlobRecord, error) {
	key, err := normalizeBlobKey(BlobKey{Digest: record.Digest})
	if err != nil {
		return BlobKey{}, BlobRecord{}, err
	}
	if record.Size < 0 {
		return BlobKey{}, BlobRecord{}, fmt.Errorf("%w: blob size cannot be negative", ErrInvalidValue)
	}
	record.Digest = key.Digest
	return key, record, nil
}

func normalizeRepoBlobRecord(record RepoBlobRecord) (RepoBlobKey, RepoBlobRecord, error) {
	key, err := normalizeRepoBlobKey(RepoBlobKey{
		Alias:      record.Alias,
		Repository: record.Repository,
		Digest:     record.Digest,
	})
	if err != nil {
		return RepoBlobKey{}, RepoBlobRecord{}, err
	}
	record.Alias = key.Alias
	record.Repository = key.Repository
	record.Digest = key.Digest
	record.Key = key.String()
	return key, record, nil
}

func normalizeRepoBlobKey(key RepoBlobKey) (RepoBlobKey, error) {
	alias, err := required("alias", key.Alias)
	if err != nil {
		return RepoBlobKey{}, err
	}
	repo, err := required("repository", key.Repository)
	if err != nil {
		return RepoBlobKey{}, err
	}
	digest, err := normalizeDigest(key.Digest)
	if err != nil {
		return RepoBlobKey{}, err
	}
	return RepoBlobKey{Alias: alias, Repository: repo, Digest: digest}, nil
}

func (k RepoBlobKey) String() string {
	return k.Alias + "/" + k.Repository + "@" + k.Digest
}

func normalizeBlobKey(key BlobKey) (BlobKey, error) {
	digest, err := normalizeDigest(key.Digest)
	if err != nil {
		return BlobKey{}, err
	}
	return BlobKey{Digest: digest}, nil
}

func required(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: %s is required", ErrInvalidKey, name)
	}
	return value, nil
}

func normalizeDigest(value string) (string, error) {
	digest, err := reference.NormalizeDigest(value)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidKey, err)
	}
	return digest, nil
}

func cloneHeaders(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		out[key] = append([]string(nil), values...)
	}
	return out
}
