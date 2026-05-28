package meta

import (
	columnx "github.com/arcgolabs/dbx/column"
	"github.com/arcgolabs/dbx/idgen"
	schemax "github.com/arcgolabs/dbx/schema"
)

type prefetchRunRow struct {
	ID                  int64  `dbx:"id"`
	Status              string `dbx:"status"`
	Trigger             string `dbx:"trigger"`
	StartedAt           int64  `dbx:"started_at"`
	FinishedAt          int64  `dbx:"finished_at"`
	ScannedRecords      int    `dbx:"scanned_records"`
	SkippedRecords      int    `dbx:"skipped_records"`
	Repositories        int    `dbx:"repositories"`
	SkippedRepositories int    `dbx:"skipped_repositories"`
	Candidates          int    `dbx:"candidates"`
	Prefetched          int    `dbx:"prefetched"`
	Failed              int    `dbx:"failed"`
	SkippedCandidates   int    `dbx:"skipped_candidates"`
	BytesWarmed         int64  `dbx:"bytes_warmed"`
	ByteBudget          int64  `dbx:"byte_budget"`
	TaskBudget          int    `dbx:"task_budget"`
	RepositoryLimit     int    `dbx:"repository_limit"`
	RetryRequested      bool   `dbx:"retry_requested"`
	Error               string `dbx:"error"`
	CreatedAt           int64  `dbx:"created_at"`
	UpdatedAt           int64  `dbx:"updated_at"`
}

type prefetchRunRowSchema struct {
	schemax.Schema[prefetchRunRow]
	ID                  columnx.IDColumn[prefetchRunRow, int64, idgen.IDSnowflake] `dbx:"id,pk"`
	Status              columnx.Column[prefetchRunRow, string]                     `dbx:"status,index"`
	Trigger             columnx.Column[prefetchRunRow, string]                     `dbx:"trigger,index"`
	StartedAt           columnx.Column[prefetchRunRow, int64]                      `dbx:"started_at,index"`
	FinishedAt          columnx.Column[prefetchRunRow, int64]                      `dbx:"finished_at,index"`
	ScannedRecords      columnx.Column[prefetchRunRow, int]                        `dbx:"scanned_records"`
	SkippedRecords      columnx.Column[prefetchRunRow, int]                        `dbx:"skipped_records"`
	Repositories        columnx.Column[prefetchRunRow, int]                        `dbx:"repositories"`
	SkippedRepositories columnx.Column[prefetchRunRow, int]                        `dbx:"skipped_repositories"`
	Candidates          columnx.Column[prefetchRunRow, int]                        `dbx:"candidates"`
	Prefetched          columnx.Column[prefetchRunRow, int]                        `dbx:"prefetched"`
	Failed              columnx.Column[prefetchRunRow, int]                        `dbx:"failed"`
	SkippedCandidates   columnx.Column[prefetchRunRow, int]                        `dbx:"skipped_candidates"`
	BytesWarmed         columnx.Column[prefetchRunRow, int64]                      `dbx:"bytes_warmed"`
	ByteBudget          columnx.Column[prefetchRunRow, int64]                      `dbx:"byte_budget"`
	TaskBudget          columnx.Column[prefetchRunRow, int]                        `dbx:"task_budget"`
	RepositoryLimit     columnx.Column[prefetchRunRow, int]                        `dbx:"repository_limit"`
	RetryRequested      columnx.Column[prefetchRunRow, bool]                       `dbx:"retry_requested"`
	Error               columnx.Column[prefetchRunRow, string]                     `dbx:"error,type=text"`
	CreatedAt           columnx.Column[prefetchRunRow, int64]                      `dbx:"created_at,index"`
	UpdatedAt           columnx.Column[prefetchRunRow, int64]                      `dbx:"updated_at,index"`
}

type prefetchControlRow struct {
	ID          int64  `dbx:"id"`
	Action      string `dbx:"action"`
	Reason      string `dbx:"reason"`
	RequestedAt int64  `dbx:"requested_at"`
	ConsumedAt  int64  `dbx:"consumed_at"`
	CreatedAt   int64  `dbx:"created_at"`
	UpdatedAt   int64  `dbx:"updated_at"`
}

type prefetchControlRowSchema struct {
	schemax.Schema[prefetchControlRow]
	ID          columnx.IDColumn[prefetchControlRow, int64, idgen.IDSnowflake] `dbx:"id,pk"`
	Action      columnx.Column[prefetchControlRow, string]                     `dbx:"action,index"`
	Reason      columnx.Column[prefetchControlRow, string]                     `dbx:"reason,type=text"`
	RequestedAt columnx.Column[prefetchControlRow, int64]                      `dbx:"requested_at,index"`
	ConsumedAt  columnx.Column[prefetchControlRow, int64]                      `dbx:"consumed_at,index"`
	CreatedAt   columnx.Column[prefetchControlRow, int64]                      `dbx:"created_at,index"`
	UpdatedAt   columnx.Column[prefetchControlRow, int64]                      `dbx:"updated_at,index"`
}
