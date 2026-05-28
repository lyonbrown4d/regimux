package meta

type PrefetchRunListOption func(*PrefetchRunListOptions)

type PrefetchRunListOptions struct {
	Limit       int
	RecentFirst bool
}

func PrefetchRunListLimit(limit int) PrefetchRunListOption {
	return func(opts *PrefetchRunListOptions) {
		opts.Limit = limit
	}
}

func PrefetchRunListRecentFirst() PrefetchRunListOption {
	return func(opts *PrefetchRunListOptions) {
		opts.RecentFirst = true
	}
}

type PrefetchOutcomeListOption func(*PrefetchOutcomeListOptions)

type PrefetchOutcomeListOptions struct {
	RunID       int64
	Limit       int
	RecentFirst bool
}

func PrefetchOutcomeListRunID(runID int64) PrefetchOutcomeListOption {
	return func(opts *PrefetchOutcomeListOptions) {
		opts.RunID = runID
	}
}

func PrefetchOutcomeListLimit(limit int) PrefetchOutcomeListOption {
	return func(opts *PrefetchOutcomeListOptions) {
		opts.Limit = limit
	}
}

func PrefetchOutcomeListRecentFirst() PrefetchOutcomeListOption {
	return func(opts *PrefetchOutcomeListOptions) {
		opts.RecentFirst = true
	}
}

type PrefetchControlListOption func(*PrefetchControlListOptions)

type PrefetchControlListOptions struct {
	Limit       int
	RecentFirst bool
}

func PrefetchControlListLimit(limit int) PrefetchControlListOption {
	return func(opts *PrefetchControlListOptions) {
		opts.Limit = limit
	}
}

func PrefetchControlListRecentFirst() PrefetchControlListOption {
	return func(opts *PrefetchControlListOptions) {
		opts.RecentFirst = true
	}
}

func prefetchRunListOptions(opts ...PrefetchRunListOption) PrefetchRunListOptions {
	out := PrefetchRunListOptions{}
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

func prefetchOutcomeListOptions(opts ...PrefetchOutcomeListOption) PrefetchOutcomeListOptions {
	out := PrefetchOutcomeListOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	if out.Limit < 0 {
		out.Limit = 0
	}
	if out.RunID < 0 {
		out.RunID = 0
	}
	return out
}

func prefetchControlListOptions(opts ...PrefetchControlListOption) PrefetchControlListOptions {
	out := PrefetchControlListOptions{}
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
