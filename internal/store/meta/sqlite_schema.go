package meta

import (
	"time"

	columnx "github.com/arcgolabs/dbx/column"
	"github.com/arcgolabs/dbx/idgen"
	schemax "github.com/arcgolabs/dbx/schema"
)

type upstreamRow struct {
	ID              int64  `dbx:"id"`
	Alias           string `dbx:"alias"`
	RepositoryCount int64  `dbx:"repository_count"`
	PullCount       int64  `dbx:"pull_count"`
	BlobBytes       int64  `dbx:"blob_bytes"`
	BlobLinkCount   int64  `dbx:"blob_link_count"`
	LastActivityAt  int64  `dbx:"last_activity_at"`
	CreatedAt       int64  `dbx:"created_at"`
	UpdatedAt       int64  `dbx:"updated_at"`
}

type upstreamRowSchema struct {
	schemax.Schema[upstreamRow]
	ID              columnx.IDColumn[upstreamRow, int64, idgen.IDSnowflake] `dbx:"id,pk"`
	Alias           columnx.Column[upstreamRow, string]                     `dbx:"alias,unique"`
	RepositoryCount columnx.Column[upstreamRow, int64]                      `dbx:"repository_count,index"`
	PullCount       columnx.Column[upstreamRow, int64]                      `dbx:"pull_count,index"`
	BlobBytes       columnx.Column[upstreamRow, int64]                      `dbx:"blob_bytes,index"`
	BlobLinkCount   columnx.Column[upstreamRow, int64]                      `dbx:"blob_link_count,index"`
	LastActivityAt  columnx.Column[upstreamRow, int64]                      `dbx:"last_activity_at,index"`
	CreatedAt       columnx.Column[upstreamRow, int64]                      `dbx:"created_at,index"`
	UpdatedAt       columnx.Column[upstreamRow, int64]                      `dbx:"updated_at,index"`
}

type repositoryRow struct {
	ID               int64  `dbx:"id"`
	Key              string `dbx:"key"`
	UpstreamID       int64  `dbx:"upstream_id"`
	Alias            string `dbx:"alias"`
	Name             string `dbx:"name"`
	PullCount        int64  `dbx:"pull_count"`
	BlobBytes        int64  `dbx:"blob_bytes"`
	BlobLinkCount    int64  `dbx:"blob_link_count"`
	LastPullAt       int64  `dbx:"last_pull_at"`
	LastBlobAccessAt int64  `dbx:"last_blob_access_at"`
	LastActivityAt   int64  `dbx:"last_activity_at"`
	CreatedAt        int64  `dbx:"created_at"`
	UpdatedAt        int64  `dbx:"updated_at"`
}

type repositoryRowSchema struct {
	schemax.Schema[repositoryRow]
	ID               columnx.IDColumn[repositoryRow, int64, idgen.IDSnowflake] `dbx:"id,pk"`
	Key              columnx.Column[repositoryRow, string]                     `dbx:"key,unique"`
	UpstreamID       columnx.Column[repositoryRow, int64]                      `dbx:"upstream_id,index"`
	Alias            columnx.Column[repositoryRow, string]                     `dbx:"alias,index"`
	Name             columnx.Column[repositoryRow, string]                     `dbx:"name,index"`
	PullCount        columnx.Column[repositoryRow, int64]                      `dbx:"pull_count,index"`
	BlobBytes        columnx.Column[repositoryRow, int64]                      `dbx:"blob_bytes,index"`
	BlobLinkCount    columnx.Column[repositoryRow, int64]                      `dbx:"blob_link_count,index"`
	LastPullAt       columnx.Column[repositoryRow, int64]                      `dbx:"last_pull_at,index"`
	LastBlobAccessAt columnx.Column[repositoryRow, int64]                      `dbx:"last_blob_access_at,index"`
	LastActivityAt   columnx.Column[repositoryRow, int64]                      `dbx:"last_activity_at,index"`
	CreatedAt        columnx.Column[repositoryRow, int64]                      `dbx:"created_at,index"`
	UpdatedAt        columnx.Column[repositoryRow, int64]                      `dbx:"updated_at,index"`
	RepositoryIndex  schemax.Index[repositoryRow]                              `idx:"name=idx_meta_repositories_upstream_name,columns=upstream_id|name"`
}

type manifestRow struct {
	ID         int64  `dbx:"id"`
	Key        string `dbx:"key"`
	Alias      string `dbx:"alias"`
	Repository string `dbx:"repository"`
	Reference  string `dbx:"reference"`
	AcceptKey  string `dbx:"accept_key"`
	Digest     string `dbx:"digest"`
	MediaType  string `dbx:"media_type"`
	Size       int64  `dbx:"size"`
	ObjectKey  string `dbx:"object_key"`
	Headers    string `dbx:"headers"`
	ExpiresAt  int64  `dbx:"expires_at"`
	CreatedAt  int64  `dbx:"created_at"`
	UpdatedAt  int64  `dbx:"updated_at"`
}

type manifestRowSchema struct {
	schemax.Schema[manifestRow]
	ID         columnx.IDColumn[manifestRow, int64, idgen.IDSnowflake] `dbx:"id,pk"`
	Key        columnx.Column[manifestRow, string]                     `dbx:"key,unique"`
	Alias      columnx.Column[manifestRow, string]                     `dbx:"alias,index"`
	Repository columnx.Column[manifestRow, string]                     `dbx:"repository,index"`
	Reference  columnx.Column[manifestRow, string]                     `dbx:"reference,index"`
	AcceptKey  columnx.Column[manifestRow, string]                     `dbx:"accept_key,index"`
	Digest     columnx.Column[manifestRow, string]                     `dbx:"digest,index"`
	MediaType  columnx.Column[manifestRow, string]                     `dbx:"media_type"`
	Size       columnx.Column[manifestRow, int64]                      `dbx:"size"`
	ObjectKey  columnx.Column[manifestRow, string]                     `dbx:"object_key"`
	Headers    columnx.Column[manifestRow, string]                     `dbx:"headers,type=text"`
	ExpiresAt  columnx.Column[manifestRow, int64]                      `dbx:"expires_at,index"`
	CreatedAt  columnx.Column[manifestRow, int64]                      `dbx:"created_at,index"`
	UpdatedAt  columnx.Column[manifestRow, int64]                      `dbx:"updated_at,index"`
	RepoIndex  schemax.Index[manifestRow]                              `idx:"name=idx_meta_manifests_repo,columns=alias|repository"`
}

type tagRow struct {
	ID         int64  `dbx:"id"`
	Key        string `dbx:"key"`
	Alias      string `dbx:"alias"`
	Repository string `dbx:"repository"`
	Reference  string `dbx:"reference"`
	Digest     string `dbx:"digest"`
	ExpiresAt  int64  `dbx:"expires_at"`
	CreatedAt  int64  `dbx:"created_at"`
	UpdatedAt  int64  `dbx:"updated_at"`
}

type tagRowSchema struct {
	schemax.Schema[tagRow]
	ID         columnx.IDColumn[tagRow, int64, idgen.IDSnowflake] `dbx:"id,pk"`
	Key        columnx.Column[tagRow, string]                     `dbx:"key,unique"`
	Alias      columnx.Column[tagRow, string]                     `dbx:"alias,index"`
	Repository columnx.Column[tagRow, string]                     `dbx:"repository,index"`
	Reference  columnx.Column[tagRow, string]                     `dbx:"reference,index"`
	Digest     columnx.Column[tagRow, string]                     `dbx:"digest,index"`
	ExpiresAt  columnx.Column[tagRow, int64]                      `dbx:"expires_at,index"`
	CreatedAt  columnx.Column[tagRow, int64]                      `dbx:"created_at,index"`
	UpdatedAt  columnx.Column[tagRow, int64]                      `dbx:"updated_at,index"`
	RepoIndex  schemax.Index[tagRow]                              `idx:"name=idx_meta_tags_repo,columns=alias|repository"`
}

type pullRow struct {
	ID                 int64  `dbx:"id"`
	Key                string `dbx:"key"`
	Alias              string `dbx:"alias"`
	Repository         string `dbx:"repository"`
	Reference          string `dbx:"reference"`
	Count              int64  `dbx:"count"`
	LastPullAt         int64  `dbx:"last_pull_at"`
	LastUpstreamPullAt int64  `dbx:"last_upstream_pull_at"`
	CreatedAt          int64  `dbx:"created_at"`
	UpdatedAt          int64  `dbx:"updated_at"`
}

type pullRowSchema struct {
	schemax.Schema[pullRow]
	ID                 columnx.IDColumn[pullRow, int64, idgen.IDSnowflake] `dbx:"id,pk"`
	Key                columnx.Column[pullRow, string]                     `dbx:"key,unique"`
	Alias              columnx.Column[pullRow, string]                     `dbx:"alias,index"`
	Repository         columnx.Column[pullRow, string]                     `dbx:"repository,index"`
	Reference          columnx.Column[pullRow, string]                     `dbx:"reference,index"`
	Count              columnx.Column[pullRow, int64]                      `dbx:"count,index"`
	LastPullAt         columnx.Column[pullRow, int64]                      `dbx:"last_pull_at,index"`
	LastUpstreamPullAt columnx.Column[pullRow, int64]                      `dbx:"last_upstream_pull_at,index"`
	CreatedAt          columnx.Column[pullRow, int64]                      `dbx:"created_at,index"`
	UpdatedAt          columnx.Column[pullRow, int64]                      `dbx:"updated_at,index"`
	RepoIndex          schemax.Index[pullRow]                              `idx:"name=idx_meta_pulls_repo,columns=alias|repository"`
}

type blobRow struct {
	ID           int64  `dbx:"id"`
	Digest       string `dbx:"digest"`
	Size         int64  `dbx:"size"`
	MediaType    string `dbx:"media_type"`
	ObjectKey    string `dbx:"object_key"`
	CreatedAt    int64  `dbx:"created_at"`
	UpdatedAt    int64  `dbx:"updated_at"`
	LastAccessAt int64  `dbx:"last_access_at"`
}

type blobRowSchema struct {
	schemax.Schema[blobRow]
	ID           columnx.IDColumn[blobRow, int64, idgen.IDSnowflake] `dbx:"id,pk"`
	Digest       columnx.Column[blobRow, string]                     `dbx:"digest,unique"`
	Size         columnx.Column[blobRow, int64]                      `dbx:"size,index"`
	MediaType    columnx.Column[blobRow, string]                     `dbx:"media_type"`
	ObjectKey    columnx.Column[blobRow, string]                     `dbx:"object_key"`
	CreatedAt    columnx.Column[blobRow, int64]                      `dbx:"created_at,index"`
	UpdatedAt    columnx.Column[blobRow, int64]                      `dbx:"updated_at,index"`
	LastAccessAt columnx.Column[blobRow, int64]                      `dbx:"last_access_at,index"`
}

type repoBlobRow struct {
	ID             int64  `dbx:"id"`
	Key            string `dbx:"key"`
	Alias          string `dbx:"alias"`
	Repository     string `dbx:"repository"`
	Digest         string `dbx:"digest"`
	SourceManifest string `dbx:"source_manifest"`
	CreatedAt      int64  `dbx:"created_at"`
	UpdatedAt      int64  `dbx:"updated_at"`
	LastAccessAt   int64  `dbx:"last_access_at"`
	LastVerifiedAt int64  `dbx:"last_verified_at"`
}

type repoBlobRowSchema struct {
	schemax.Schema[repoBlobRow]
	ID             columnx.IDColumn[repoBlobRow, int64, idgen.IDSnowflake] `dbx:"id,pk"`
	Key            columnx.Column[repoBlobRow, string]                     `dbx:"key,unique"`
	Alias          columnx.Column[repoBlobRow, string]                     `dbx:"alias,index"`
	Repository     columnx.Column[repoBlobRow, string]                     `dbx:"repository,index"`
	Digest         columnx.Column[repoBlobRow, string]                     `dbx:"digest,index"`
	SourceManifest columnx.Column[repoBlobRow, string]                     `dbx:"source_manifest,index"`
	CreatedAt      columnx.Column[repoBlobRow, int64]                      `dbx:"created_at,index"`
	UpdatedAt      columnx.Column[repoBlobRow, int64]                      `dbx:"updated_at,index"`
	LastAccessAt   columnx.Column[repoBlobRow, int64]                      `dbx:"last_access_at,index"`
	LastVerifiedAt columnx.Column[repoBlobRow, int64]                      `dbx:"last_verified_at,index"`
	RepoIndex      schemax.Index[repoBlobRow]                              `idx:"name=idx_meta_repo_blobs_repo,columns=alias|repository"`
}

var (
	sqliteUpstreamRows        = schemax.MustSchema("meta_upstreams", upstreamRowSchema{})
	sqliteRepositoryRows      = schemax.MustSchema("meta_repositories", repositoryRowSchema{})
	sqliteManifestRows        = schemax.MustSchema("meta_manifests", manifestRowSchema{})
	sqliteTagRows             = schemax.MustSchema("meta_tags", tagRowSchema{})
	sqlitePullRows            = schemax.MustSchema("meta_pulls", pullRowSchema{})
	sqliteBlobRows            = schemax.MustSchema("meta_blobs", blobRowSchema{})
	sqliteRepoBlobRows        = schemax.MustSchema("meta_repo_blobs", repoBlobRowSchema{})
	sqlitePrefetchRunRows     = schemax.MustSchema("meta_prefetch_runs", prefetchRunRowSchema{})
	sqlitePrefetchOutcomeRows = schemax.MustSchema("meta_prefetch_outcomes", prefetchOutcomeRowSchema{})
	sqlitePrefetchControlRows = schemax.MustSchema("meta_prefetch_controls", prefetchControlRowSchema{})
	sqliteEndpointHealthRows  = schemax.MustSchema("meta_endpoint_health", endpointHealthRowSchema{})
)

func unixNano(t time.Time) int64 {
	t = t.UTC()
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func timeFromUnixNano(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(0, value).UTC()
}
