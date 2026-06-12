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

	CatalogRepository
	EndpointHealthRepository
	ManifestRepository
	TagRepository
	PullRepository
	BlobRepository
	RepoBlobRepository
	PrefetchRepository
	RefreshRepository
	MetadataReadModel
}

type CatalogRepository interface {
	UpstreamByAlias(ctx context.Context, alias string) (*Upstream, error)
	ListUpstreams(ctx context.Context, opts ...UpstreamListOption) ([]Upstream, error)
	RepositoryByName(ctx context.Context, upstreamID int64, name string) (*Repository, error)
	ListRepositories(ctx context.Context, opts ...RepositoryListOption) ([]Repository, error)
}

type EndpointHealthRepository interface {
	EndpointHealth(ctx context.Context, key EndpointHealthKey) (*EndpointHealthRecord, bool, error)
	UpsertEndpointHealth(ctx context.Context, record EndpointHealthRecord) (*EndpointHealthRecord, error)
	ListEndpointHealth(ctx context.Context, opts ...EndpointHealthListOption) ([]EndpointHealthRecord, error)
}

type ManifestRepository interface {
	Manifest(ctx context.Context, key ManifestKey) (*ManifestRecord, bool, error)
	UpsertManifest(ctx context.Context, record ManifestRecord) (*ManifestRecord, error)
	DeleteManifest(ctx context.Context, key ManifestKey) error
	GetManifest(ctx context.Context, key string) (*ManifestRecord, bool, error)
	PutManifest(ctx context.Context, record ManifestRecord) error
	ListManifests(ctx context.Context) ([]ManifestRecord, error)
}

type TagRepository interface {
	Tag(ctx context.Context, key TagKey) (*TagRecord, bool, error)
	UpsertTag(ctx context.Context, record TagRecord) (*TagRecord, error)
	DeleteTag(ctx context.Context, key TagKey) error
	GetTag(ctx context.Context, key string) (*TagRecord, bool, error)
	PutTag(ctx context.Context, record TagRecord) error
	ListTags(ctx context.Context) ([]TagRecord, error)
}

type PullRepository interface {
	Pull(ctx context.Context, key PullKey) (*PullRecord, bool, error)
	RecordPull(ctx context.Context, key PullKey, at time.Time) (*PullRecord, error)
	RecordUpstreamPull(ctx context.Context, key PullKey, at time.Time) (*PullRecord, error)
	RecordPolicyDeniedPull(ctx context.Context, key PullKey, at time.Time) (*PullRecord, error)
	ListPulls(ctx context.Context, opts ...PullListOption) ([]PullRecord, error)
}

type BlobRepository interface {
	Blob(ctx context.Context, key BlobKey) (*BlobRecord, bool, error)
	UpsertBlob(ctx context.Context, record BlobRecord) (*BlobRecord, error)
	DeleteBlob(ctx context.Context, key BlobKey) error
	GetBlob(ctx context.Context, digest string) (*BlobRecord, bool, error)
	PutBlob(ctx context.Context, record BlobRecord) error
	ListBlobs(ctx context.Context, opts ...BlobListOption) ([]BlobRecord, error)
}

type RepoBlobRepository interface {
	RepoBlob(ctx context.Context, key RepoBlobKey) (*RepoBlobRecord, bool, error)
	UpsertRepoBlob(ctx context.Context, record RepoBlobRecord) (*RepoBlobRecord, error)
	DeleteRepoBlob(ctx context.Context, key RepoBlobKey) error
	ListRepoBlobs(ctx context.Context, opts ...RepoBlobListOption) ([]RepoBlobRecord, error)
}

type PrefetchRepository interface {
	CreatePrefetchRun(ctx context.Context, record PrefetchRunRecord) (*PrefetchRunRecord, error)
	UpdatePrefetchRun(ctx context.Context, record PrefetchRunRecord) (*PrefetchRunRecord, error)
	ListPrefetchRuns(ctx context.Context, opts ...PrefetchRunListOption) ([]PrefetchRunRecord, error)
	CreatePrefetchOutcome(ctx context.Context, record PrefetchOutcomeRecord) (*PrefetchOutcomeRecord, error)
	LatestPrefetchOutcome(ctx context.Context, key PrefetchCandidateKey) (*PrefetchOutcomeRecord, bool, error)
	ListPrefetchOutcomes(ctx context.Context, opts ...PrefetchOutcomeListOption) ([]PrefetchOutcomeRecord, error)
	RequestPrefetchControl(ctx context.Context, record PrefetchControlRecord) (*PrefetchControlRecord, error)
	ConsumePrefetchControl(ctx context.Context, action string, at time.Time) (*PrefetchControlRecord, bool, error)
	ListPrefetchControls(ctx context.Context, opts ...PrefetchControlListOption) ([]PrefetchControlRecord, error)
}

type RefreshRepository interface {
	RefreshIntent(ctx context.Context, key RefreshIntentKey) (*RefreshIntentRecord, bool, error)
	QueueRefreshIntent(ctx context.Context, record RefreshIntentRecord, at time.Time, window time.Duration) (*RefreshIntentRecord, bool, error)
	ConsumeDueRefreshIntents(ctx context.Context, at time.Time, limit int) ([]RefreshIntentRecord, error)
}

type MetadataReadModel interface {
	MetadataStats(ctx context.Context, now time.Time) (MetadataStats, error)
}

type MetadataStats struct {
	ManifestCount          int64
	ExpiredManifestCount   int64
	ManifestBytes          int64
	TagCount               int64
	ExpiredTagCount        int64
	BlobCount              int64
	BlobBytes              int64
	RepoBlobCount          int64
	PullCount              int64
	PolicyDeniedPullCount  int64
	RepositoryCount        int64
	RepositoryBytes        int64
	LastPullAt             time.Time
	LastUpstreamPullAt     time.Time
	LastPolicyDeniedPullAt time.Time
}

type Upstream struct {
	ID                    int64     `json:"id,omitempty"`
	Alias                 string    `json:"alias"`
	RepositoryCount       int64     `json:"repository_count"`
	PullCount             int64     `json:"pull_count"`
	PolicyDeniedPullCount int64     `json:"policy_denied_pull_count"`
	BlobBytes             int64     `json:"blob_bytes"`
	BlobLinkCount         int64     `json:"blob_link_count"`
	LastPolicyDeniedAt    time.Time `json:"last_policy_denied_at,omitzero"`
	LastActivityAt        time.Time `json:"last_activity_at,omitzero"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type Repository struct {
	ID                    int64     `json:"id,omitempty"`
	UpstreamID            int64     `json:"upstream_id"`
	Alias                 string    `json:"alias"`
	Name                  string    `json:"name"`
	PullCount             int64     `json:"pull_count"`
	PolicyDeniedPullCount int64     `json:"policy_denied_pull_count"`
	BlobBytes             int64     `json:"blob_bytes"`
	BlobLinkCount         int64     `json:"blob_link_count"`
	LastPullAt            time.Time `json:"last_pull_at,omitzero"`
	LastPolicyDeniedAt    time.Time `json:"last_policy_denied_at,omitzero"`
	LastBlobAccessAt      time.Time `json:"last_blob_access_at,omitzero"`
	LastActivityAt        time.Time `json:"last_activity_at,omitzero"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type ManifestKey struct {
	Alias      string `json:"alias"`
	Repository string `json:"repository"`
	Digest     string `json:"digest"`
}

type ManifestRecord struct {
	ID         int64               `json:"id,omitempty"`
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
	ID         int64     `json:"id,omitempty"`
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
	ID                 int64     `json:"id,omitempty"`
	Key                string    `json:"key,omitempty"`
	Alias              string    `json:"alias"`
	Repository         string    `json:"repository"`
	Reference          string    `json:"reference"`
	Count              int64     `json:"count"`
	PolicyDeniedCount  int64     `json:"policy_denied_count"`
	LastPullAt         time.Time `json:"last_pull_at,omitzero"`
	LastUpstreamPullAt time.Time `json:"last_upstream_pull_at,omitzero"`
	LastPolicyDeniedAt time.Time `json:"last_policy_denied_at,omitzero"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type BlobKey struct {
	Digest string `json:"digest"`
}

type BlobRecord struct {
	ID           int64     `json:"id,omitempty"`
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
	ID             int64     `json:"id,omitempty"`
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
