package meta

import (
	columnx "github.com/arcgolabs/dbx/column"
	"github.com/arcgolabs/dbx/idgen"
	schemax "github.com/arcgolabs/dbx/schema"
)

type prefetchOutcomeRow struct {
	ID                 int64  `dbx:"id"`
	RunID              int64  `dbx:"run_id"`
	CandidateKey       string `dbx:"candidate_key"`
	Alias              string `dbx:"alias"`
	Repository         string `dbx:"repository"`
	Reference          string `dbx:"reference"`
	SourceReference    string `dbx:"source_reference"`
	Status             string `dbx:"status"`
	Reason             string `dbx:"reason"`
	Score              int    `dbx:"score"`
	ManifestDigest     string `dbx:"manifest_digest"`
	LayerCount         int    `dbx:"layer_count"`
	BlobCount          int    `dbx:"blob_count"`
	ChildManifestCount int    `dbx:"child_manifest_count"`
	BytesWarmed        int64  `dbx:"bytes_warmed"`
	Attempt            int    `dbx:"attempt"`
	Error              string `dbx:"error"`
	SkipReason         string `dbx:"skip_reason"`
	NextRetryAt        int64  `dbx:"next_retry_at"`
	StartedAt          int64  `dbx:"started_at"`
	FinishedAt         int64  `dbx:"finished_at"`
	CreatedAt          int64  `dbx:"created_at"`
}

type prefetchOutcomeRowSchema struct {
	schemax.Schema[prefetchOutcomeRow]
	ID                 columnx.IDColumn[prefetchOutcomeRow, int64, idgen.IDSnowflake] `dbx:"id,pk"`
	RunID              columnx.Column[prefetchOutcomeRow, int64]                      `dbx:"run_id,index"`
	CandidateKey       columnx.Column[prefetchOutcomeRow, string]                     `dbx:"candidate_key,index"`
	Alias              columnx.Column[prefetchOutcomeRow, string]                     `dbx:"alias,index"`
	Repository         columnx.Column[prefetchOutcomeRow, string]                     `dbx:"repository,index"`
	Reference          columnx.Column[prefetchOutcomeRow, string]                     `dbx:"reference,index"`
	SourceReference    columnx.Column[prefetchOutcomeRow, string]                     `dbx:"source_reference,index"`
	Status             columnx.Column[prefetchOutcomeRow, string]                     `dbx:"status,index"`
	Reason             columnx.Column[prefetchOutcomeRow, string]                     `dbx:"reason,type=text"`
	Score              columnx.Column[prefetchOutcomeRow, int]                        `dbx:"score"`
	ManifestDigest     columnx.Column[prefetchOutcomeRow, string]                     `dbx:"manifest_digest,index"`
	LayerCount         columnx.Column[prefetchOutcomeRow, int]                        `dbx:"layer_count"`
	BlobCount          columnx.Column[prefetchOutcomeRow, int]                        `dbx:"blob_count"`
	ChildManifestCount columnx.Column[prefetchOutcomeRow, int]                        `dbx:"child_manifest_count"`
	BytesWarmed        columnx.Column[prefetchOutcomeRow, int64]                      `dbx:"bytes_warmed"`
	Attempt            columnx.Column[prefetchOutcomeRow, int]                        `dbx:"attempt"`
	Error              columnx.Column[prefetchOutcomeRow, string]                     `dbx:"error,type=text"`
	SkipReason         columnx.Column[prefetchOutcomeRow, string]                     `dbx:"skip_reason,type=text"`
	NextRetryAt        columnx.Column[prefetchOutcomeRow, int64]                      `dbx:"next_retry_at,index"`
	StartedAt          columnx.Column[prefetchOutcomeRow, int64]                      `dbx:"started_at,index"`
	FinishedAt         columnx.Column[prefetchOutcomeRow, int64]                      `dbx:"finished_at,index"`
	CreatedAt          columnx.Column[prefetchOutcomeRow, int64]                      `dbx:"created_at,index"`
	RepoIndex          schemax.Index[prefetchOutcomeRow]                              `idx:"name=idx_meta_prefetch_outcomes_repo,columns=alias|repository"`
}
