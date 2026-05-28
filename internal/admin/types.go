package admin

type PageData struct {
	Title              string
	Active             string
	ActiveLabel        string
	GeneratedAt        string
	BasePath           string
	Locale             string
	HTMLLang           string
	LanguageSwitchHref string

	Summary       Summary
	Upstreams     []UpstreamRow
	RecentPulls   []PullRow
	Pulls         []PullRow
	Cache         CacheSummary
	Activity      ActivitySummary
	Storage       StorageSummary
	Audit         AuditSummary
	Scheduler     SchedulerSummary
	ConfigRows    []ConfigRow
	ConfigSources []ConfigSourceRow
	Sync          SyncPageData
}

type Summary struct {
	Version            string
	Uptime             string
	Listen             string
	PublicURL          string
	AuthEnabled        bool
	CacheBackend       string
	SchedulerEnabled   bool
	DistributedLock    bool
	UpstreamCount      int
	MirrorCount        int
	ManifestCount      int
	TagCount           int
	BlobCount          int
	RepoBlobCount      int
	RepositoryCount    int
	RepositoryBytes    string
	BlobBytes          string
	PullCount          int
	LastPullAt         string
	LastUpstreamPullAt string
}

type UpstreamRow struct {
	Alias            string
	Registry         string
	DefaultNamespace string
	AuthType         string
	MirrorPolicy     string
	BlobPolicy       string
	ProbeEnabled     bool
	MirrorCount      int
	RepositoryCount  int
	PullCount        int64
	BlobBytes        string
	LastActivityAt   string
	Endpoints        []EndpointRow
}

type EndpointRow struct {
	Registry      string
	Role          string
	Latency       string
	Score         string
	Inflight      int
	Failures      int
	SuccessRate   string
	Mismatches    int64
	Cooldown      string
	Degraded      string
	LastSuccessAt string
	LastFailureAt string
	Status        string
}

type PullRow struct {
	Key                string
	Alias              string
	Repository         string
	Reference          string
	Count              int64
	LastPullAt         string
	LastUpstreamPullAt string
}

type ActivitySummary struct {
	RequestAuditAvailable bool
	Rows                  []ActivityRow
}

type ActivityRow struct {
	OccurredAt string
	Event      string
	Actor      string
	Method     string
	Path       string
	Alias      string
	Repository string
	Reference  string
	Count      int64
	UpstreamAt string
	Source     string
	RequestID  string
}

type CacheSummary struct {
	ManifestCount        int
	ExpiredManifestCount int
	TagCount             int
	ExpiredTagCount      int
	BlobCount            int
	BlobBytes            string
	RepoBlobCount        int
	RecentBlobs          []BlobRow
}

type BlobRow struct {
	Digest       string
	Size         string
	MediaType    string
	LastAccessAt string
	UpdatedAt    string
}

type StorageSummary struct {
	TotalBytes    string
	BlobBytes     string
	ManifestBytes string
	BlobCount     int
	ManifestCount int
	RepoBlobCount int
	RecentBlobs   []BlobRow
	LargeBlobs    []BlobRow
	Repositories  []RepositoryRow
	RepoBlobLinks []RepoBlobRow
}

type RepositoryRow struct {
	Alias            string
	Repository       string
	PullCount        int64
	BlobBytes        string
	BlobLinkCount    int64
	LastPullAt       string
	LastBlobAccessAt string
	LastActivityAt   string
}

type RepoBlobRow struct {
	Key            string
	Alias          string
	Repository     string
	Digest         string
	SourceManifest string
	LastAccessAt   string
	LastVerifiedAt string
	UpdatedAt      string
}

type AuditSummary struct {
	AuthEnabled        bool
	UserCount          int
	Users              []AuditUserRow
	RecentLogins       []AuditLoginRow
	LoginDataAvailable bool
}

type AuditUserRow struct {
	Username         string
	RepositoryScopes string
	Groups           string
	Credential       string
}

type AuditLoginRow struct {
	Username  string
	Remote    string
	UserAgent string
	At        string
}

type SchedulerSummary struct {
	Enabled                      bool
	DistributedLock              bool
	LockTTL                      string
	CleanupEnabled               bool
	CleanupInterval              string
	CleanupUnusedFor             string
	CleanupMaxScan               int
	CleanupMaxDeletes            int
	CleanupMaxBytes              string
	CleanupTargetBytes           string
	CleanupDryRun                bool
	PrefetchEnabled              bool
	PrefetchInterval             string
	PrefetchMinPullCount         int64
	PrefetchMaxRecords           int
	PrefetchMaxCandidatesPerRepo int
	PrefetchMaxVersionDistance   int
	PrefetchMaxBytes             string
	PrefetchMaxTasks             int
	PrefetchMaxRepositories      int
	PrefetchFailureBackoff       string
	PrefetchRetryWindow          string
	PrefetchRuns                 []PrefetchRunRow
	PrefetchOutcomes             []PrefetchOutcomeRow
	PrefetchControlMessage       string
	PrefetchControlError         string
	ProbeJobs                    []ProbeJobRow
}

type PrefetchRunRow struct {
	ID                  int64
	Status              string
	StartedAt           string
	FinishedAt          string
	ScannedRecords      int
	Repositories        int
	SkippedRepositories int
	Candidates          int
	Prefetched          int
	Failed              int
	SkippedCandidates   int
	BytesWarmed         string
	RetryRequested      bool
	Error               string
}

type PrefetchOutcomeRow struct {
	Candidate      string
	Status         string
	Attempt        int
	Reason         string
	SkipReason     string
	Error          string
	NextRetryAt    string
	FinishedAt     string
	BytesWarmed    string
	ManifestDigest string
}

type ProbeJobRow struct {
	Alias    string
	Enabled  bool
	Interval string
	Timeout  string
	Cooldown string
	Jitter   string
}

type ConfigRow struct {
	Path  string
	Value string
}

type ConfigSourceRow struct {
	Name   string
	Status string
	Detail string
}

type SyncPageData struct {
	Form      SyncForm
	Result    SyncResult
	Error     string
	HasResult bool
}

type SyncForm struct {
	UpstreamAlias string
	Repository    string
	Reference     string
}

type SyncResult struct {
	Alias              string
	Repository         string
	Reference          string
	ManifestDigest     string
	MediaType          string
	LayerCount         int
	BlobCount          int
	ChildManifestCount int
	Duration           string
}
