package depprefetch

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (s *Service) candidates(records *collectionlist.List[meta.PullRecord], opts ecosystem.PrefetchOptions) *collectionlist.List[Candidate] {
	if records == nil {
		return collectionlist.NewList[Candidate]()
	}
	seenRepositories := map[string]struct{}{}
	candidates := collectionlist.NewListWithCapacity[Candidate](records.Len())
	limit := candidateLimit(opts)
	records.Range(func(_ int, record meta.PullRecord) bool {
		candidate, ok := s.candidate(record, opts, seenRepositories)
		if !ok {
			return true
		}
		candidates.Add(candidate)
		return candidates.Len() < limit
	})
	return candidates
}

func (s *Service) candidate(record meta.PullRecord, opts ecosystem.PrefetchOptions, seenRepositories map[string]struct{}) (Candidate, bool) {
	alias, ok := rawAlias(s.ecosystem, record.Alias)
	if !ok || record.Count < opts.MinPullCount || record.Reference == "" || record.Repository == "" {
		return Candidate{}, false
	}
	candidate := Candidate{
		ScopedAlias: record.Alias,
		Alias:       alias,
		Repository:  record.Repository,
		Reference:   record.Reference,
		Count:       record.Count,
		Score:       candidateScore(record),
	}
	key := groupKey(candidate)
	if _, exists := seenRepositories[key]; !exists {
		if opts.MaxRepositories > 0 && len(seenRepositories) >= opts.MaxRepositories {
			return Candidate{}, false
		}
		seenRepositories[key] = struct{}{}
	}
	return candidate, true
}

func candidateScore(record meta.PullRecord) int {
	if record.Count <= 0 {
		return 1
	}
	if record.Count > int64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(record.Count)
}

func candidateLimit(opts ecosystem.PrefetchOptions) int {
	limit := opts.MaxRecords
	if opts.MaxTasks > 0 && opts.MaxTasks < limit {
		return opts.MaxTasks
	}
	return limit
}
