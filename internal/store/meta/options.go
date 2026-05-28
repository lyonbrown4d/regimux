package meta

type EndpointHealthListOption func(*EndpointHealthListOptions)

type EndpointHealthListOptions struct {
	Alias string
	Limit int
}

func EndpointHealthListAlias(alias string) EndpointHealthListOption {
	return func(opts *EndpointHealthListOptions) {
		opts.Alias = alias
	}
}

func EndpointHealthListLimit(limit int) EndpointHealthListOption {
	return func(opts *EndpointHealthListOptions) {
		opts.Limit = limit
	}
}

type UpstreamListOption func(*UpstreamListOptions)

type UpstreamListOptions struct {
	Limit       int
	RecentFirst bool
}

func UpstreamListLimit(limit int) UpstreamListOption {
	return func(opts *UpstreamListOptions) {
		opts.Limit = limit
	}
}

func UpstreamListRecentFirst() UpstreamListOption {
	return func(opts *UpstreamListOptions) {
		opts.RecentFirst = true
	}
}

type RepositoryListOption func(*RepositoryListOptions)

type RepositoryListOptions struct {
	Limit       int
	RecentFirst bool
}

func RepositoryListLimit(limit int) RepositoryListOption {
	return func(opts *RepositoryListOptions) {
		opts.Limit = limit
	}
}

func RepositoryListRecentFirst() RepositoryListOption {
	return func(opts *RepositoryListOptions) {
		opts.RecentFirst = true
	}
}

type PullListOption func(*PullListOptions)

type PullListOptions struct {
	Limit       int
	RecentFirst bool
}

func PullListLimit(limit int) PullListOption {
	return func(opts *PullListOptions) {
		opts.Limit = limit
	}
}

func PullListRecentFirst() PullListOption {
	return func(opts *PullListOptions) {
		opts.RecentFirst = true
	}
}

type BlobListOption func(*BlobListOptions)

type BlobListOrder int

const (
	BlobListDefault BlobListOrder = iota
	BlobListRecentFirst
	BlobListLargestFirst
)

type BlobListOptions struct {
	Limit int
	Order BlobListOrder
}

func BlobListLimit(limit int) BlobListOption {
	return func(opts *BlobListOptions) {
		opts.Limit = limit
	}
}

func BlobListOrderByRecent() BlobListOption {
	return func(opts *BlobListOptions) {
		opts.Order = BlobListRecentFirst
	}
}

func BlobListOrderByLargest() BlobListOption {
	return func(opts *BlobListOptions) {
		opts.Order = BlobListLargestFirst
	}
}

type RepoBlobListOption func(*RepoBlobListOptions)

type RepoBlobListOptions struct {
	Limit       int
	RecentFirst bool
}

func RepoBlobListLimit(limit int) RepoBlobListOption {
	return func(opts *RepoBlobListOptions) {
		opts.Limit = limit
	}
}

func RepoBlobListRecentFirst() RepoBlobListOption {
	return func(opts *RepoBlobListOptions) {
		opts.RecentFirst = true
	}
}

func pullListOptions(opts ...PullListOption) PullListOptions {
	out := PullListOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	if out.Limit < 0 {
		out.Limit = 0
	}
	return out
}

func blobListOptions(opts ...BlobListOption) BlobListOptions {
	out := BlobListOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	if out.Limit < 0 {
		out.Limit = 0
	}
	return out
}

func repoBlobListOptions(opts ...RepoBlobListOption) RepoBlobListOptions {
	out := RepoBlobListOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	if out.Limit < 0 {
		out.Limit = 0
	}
	return out
}

func endpointHealthListOptions(opts ...EndpointHealthListOption) EndpointHealthListOptions {
	out := EndpointHealthListOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	if out.Limit < 0 {
		out.Limit = 0
	}
	return out
}

func upstreamListOptions(opts ...UpstreamListOption) UpstreamListOptions {
	out := UpstreamListOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	if out.Limit < 0 {
		out.Limit = 0
	}
	return out
}

func repositoryListOptions(opts ...RepositoryListOption) RepositoryListOptions {
	out := RepositoryListOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	if out.Limit < 0 {
		out.Limit = 0
	}
	return out
}
