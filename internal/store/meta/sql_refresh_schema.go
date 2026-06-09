package meta

import (
	columnx "github.com/arcgolabs/dbx/column"
	"github.com/arcgolabs/dbx/idgen"
	schemax "github.com/arcgolabs/dbx/schema"
)

type refreshIntentRow struct {
	ID         int64  `dbx:"id"`
	Key        string `dbx:"key"`
	Ecosystem  string `dbx:"ecosystem"`
	Kind       string `dbx:"kind"`
	Alias      string `dbx:"alias"`
	Repository string `dbx:"repository"`
	Reference  string `dbx:"reference"`
	Accept     string `dbx:"accept"`
	DueAt      int64  `dbx:"due_at"`
	LastSeenAt int64  `dbx:"last_seen_at"`
	Skipped    int    `dbx:"skipped"`
	CreatedAt  int64  `dbx:"created_at"`
	UpdatedAt  int64  `dbx:"updated_at"`
}

type refreshIntentRowSchema struct {
	schemax.Schema[refreshIntentRow]
	ID         columnx.IDColumn[refreshIntentRow, int64, idgen.IDSnowflake] `dbx:"id,pk"`
	Key        columnx.Column[refreshIntentRow, string]                     `dbx:"key,unique"`
	Ecosystem  columnx.Column[refreshIntentRow, string]                     `dbx:"ecosystem,index"`
	Kind       columnx.Column[refreshIntentRow, string]                     `dbx:"kind,index"`
	Alias      columnx.Column[refreshIntentRow, string]                     `dbx:"alias,index"`
	Repository columnx.Column[refreshIntentRow, string]                     `dbx:"repository,index"`
	Reference  columnx.Column[refreshIntentRow, string]                     `dbx:"reference,index"`
	Accept     columnx.Column[refreshIntentRow, string]                     `dbx:"accept,type=text"`
	DueAt      columnx.Column[refreshIntentRow, int64]                      `dbx:"due_at,index"`
	LastSeenAt columnx.Column[refreshIntentRow, int64]                      `dbx:"last_seen_at,index"`
	Skipped    columnx.Column[refreshIntentRow, int]                        `dbx:"skipped"`
	CreatedAt  columnx.Column[refreshIntentRow, int64]                      `dbx:"created_at,index"`
	UpdatedAt  columnx.Column[refreshIntentRow, int64]                      `dbx:"updated_at,index"`
	Target     schemax.Index[refreshIntentRow]                              `idx:"name=idx_meta_refresh_intents_target,columns=ecosystem|alias|repository|reference"`
}
