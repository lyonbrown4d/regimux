package meta

import (
	columnx "github.com/arcgolabs/dbx/column"
	"github.com/arcgolabs/dbx/idgen"
	schemax "github.com/arcgolabs/dbx/schema"
)

type endpointHealthRow struct {
	ID                   int64  `dbx:"id"`
	Key                  string `dbx:"key"`
	Alias                string `dbx:"alias"`
	Registry             string `dbx:"registry"`
	Repository           string `dbx:"repository"`
	LatencyEWMA          int64  `dbx:"latency_ewma"`
	LatencySamples       int64  `dbx:"latency_samples"`
	ConsecutiveFailures  int64  `dbx:"consecutive_failures"`
	SuccessCount         int64  `dbx:"success_count"`
	FailureCount         int64  `dbx:"failure_count"`
	ContentMismatchCount int64  `dbx:"content_mismatch_count"`
	CooldownUntil        int64  `dbx:"cooldown_until"`
	DegradedUntil        int64  `dbx:"degraded_until"`
	LastSuccessAt        int64  `dbx:"last_success_at"`
	LastFailureAt        int64  `dbx:"last_failure_at"`
	LastProbeAt          int64  `dbx:"last_probe_at"`
	CreatedAt            int64  `dbx:"created_at"`
	UpdatedAt            int64  `dbx:"updated_at"`
}

type endpointHealthRowSchema struct {
	schemax.Schema[endpointHealthRow]
	ID                   columnx.IDColumn[endpointHealthRow, int64, idgen.IDSnowflake] `dbx:"id,pk"`
	Key                  columnx.Column[endpointHealthRow, string]                     `dbx:"key,unique"`
	Alias                columnx.Column[endpointHealthRow, string]                     `dbx:"alias,index"`
	Registry             columnx.Column[endpointHealthRow, string]                     `dbx:"registry,index"`
	Repository           columnx.Column[endpointHealthRow, string]                     `dbx:"repository,index"`
	LatencyEWMA          columnx.Column[endpointHealthRow, int64]                      `dbx:"latency_ewma"`
	LatencySamples       columnx.Column[endpointHealthRow, int64]                      `dbx:"latency_samples"`
	ConsecutiveFailures  columnx.Column[endpointHealthRow, int64]                      `dbx:"consecutive_failures,index"`
	SuccessCount         columnx.Column[endpointHealthRow, int64]                      `dbx:"success_count"`
	FailureCount         columnx.Column[endpointHealthRow, int64]                      `dbx:"failure_count"`
	ContentMismatchCount columnx.Column[endpointHealthRow, int64]                      `dbx:"content_mismatch_count"`
	CooldownUntil        columnx.Column[endpointHealthRow, int64]                      `dbx:"cooldown_until,index"`
	DegradedUntil        columnx.Column[endpointHealthRow, int64]                      `dbx:"degraded_until,index"`
	LastSuccessAt        columnx.Column[endpointHealthRow, int64]                      `dbx:"last_success_at,index"`
	LastFailureAt        columnx.Column[endpointHealthRow, int64]                      `dbx:"last_failure_at,index"`
	LastProbeAt          columnx.Column[endpointHealthRow, int64]                      `dbx:"last_probe_at,index"`
	CreatedAt            columnx.Column[endpointHealthRow, int64]                      `dbx:"created_at,index"`
	UpdatedAt            columnx.Column[endpointHealthRow, int64]                      `dbx:"updated_at,index"`
	EndpointIndex        schemax.Index[endpointHealthRow]                              `idx:"name=idx_meta_endpoint_health_endpoint,columns=alias|registry"`
	RepositoryIndex      schemax.Index[endpointHealthRow]                              `idx:"name=idx_meta_endpoint_health_repo,columns=alias|registry|repository"`
}
