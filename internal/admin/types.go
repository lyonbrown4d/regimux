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
	Endpoints        []EndpointRow
}

type EndpointRow struct {
	Registry      string
	Role          string
	Latency       string
	Score         string
	Inflight      int
	Failures      int
	Cooldown      string
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
	RepoBlobLinks []RepoBlobRow
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
	CleanupDryRun                bool
	PrefetchEnabled              bool
	PrefetchInterval             string
	PrefetchMinPullCount         int64
	PrefetchMaxRecords           int
	PrefetchMaxCandidatesPerRepo int
	PrefetchMaxVersionDistance   int
	ProbeJobs                    []ProbeJobRow
}

type ProbeJobRow struct {
	Alias    string
	Enabled  bool
	Interval string
	Timeout  string
	Cooldown string
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
