package admin

type SyncPageData struct {
	Form      SyncForm
	Upstreams []SyncUpstreamOption
	Result    SyncResult
	Job       SyncJobView
	Error     string
	HasResult bool
	HasJob    bool
}

type SyncForm struct {
	UpstreamAlias string
	Repository    string
	Reference     string
}

type SyncUpstreamOption struct {
	Alias    string
	Registry string
	Selected bool
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

type SyncJobView struct {
	ID         string
	Status     string
	Target     string
	Error      string
	CreatedAt  string
	StartedAt  string
	FinishedAt string
	Poll       bool
	Result     SyncResult
	HasResult  bool
}
