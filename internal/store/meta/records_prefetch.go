package meta

import "time"

type PrefetchRunRecord struct {
	ID                  int64     `json:"id,omitempty"`
	Status              string    `json:"status"`
	Trigger             string    `json:"trigger,omitempty"`
	StartedAt           time.Time `json:"started_at,omitzero"`
	FinishedAt          time.Time `json:"finished_at,omitzero"`
	ScannedRecords      int       `json:"scanned_records"`
	SkippedRecords      int       `json:"skipped_records"`
	Repositories        int       `json:"repositories"`
	SkippedRepositories int       `json:"skipped_repositories"`
	Candidates          int       `json:"candidates"`
	Prefetched          int       `json:"prefetched"`
	Failed              int       `json:"failed"`
	SkippedCandidates   int       `json:"skipped_candidates"`
	BytesWarmed         int64     `json:"bytes_warmed"`
	ByteBudget          int64     `json:"byte_budget"`
	TaskBudget          int       `json:"task_budget"`
	RepositoryLimit     int       `json:"repository_limit"`
	RetryRequested      bool      `json:"retry_requested"`
	Error               string    `json:"error,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type PrefetchCandidateKey struct {
	Alias      string `json:"alias"`
	Repository string `json:"repository"`
	Reference  string `json:"reference"`
}

type PrefetchOutcomeRecord struct {
	ID                 int64     `json:"id,omitempty"`
	RunID              int64     `json:"run_id"`
	CandidateKey       string    `json:"candidate_key,omitempty"`
	Alias              string    `json:"alias"`
	Repository         string    `json:"repository"`
	Reference          string    `json:"reference"`
	SourceReference    string    `json:"source_reference,omitempty"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Score              int       `json:"score"`
	ManifestDigest     string    `json:"manifest_digest,omitempty"`
	LayerCount         int       `json:"layer_count"`
	BlobCount          int       `json:"blob_count"`
	ChildManifestCount int       `json:"child_manifest_count"`
	BytesWarmed        int64     `json:"bytes_warmed"`
	Attempt            int       `json:"attempt"`
	Error              string    `json:"error,omitempty"`
	SkipReason         string    `json:"skip_reason,omitempty"`
	NextRetryAt        time.Time `json:"next_retry_at,omitzero"`
	StartedAt          time.Time `json:"started_at,omitzero"`
	FinishedAt         time.Time `json:"finished_at,omitzero"`
	CreatedAt          time.Time `json:"created_at"`
}

type PrefetchControlRecord struct {
	ID          int64     `json:"id,omitempty"`
	Action      string    `json:"action"`
	Reason      string    `json:"reason,omitempty"`
	RequestedAt time.Time `json:"requested_at,omitzero"`
	ConsumedAt  time.Time `json:"consumed_at,omitzero"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (k PrefetchCandidateKey) String() string {
	return k.Alias + "/" + k.Repository + ":" + k.Reference
}
