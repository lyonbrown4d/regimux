package depprefetch

import (
  collectionlist "github.com/arcgolabs/collectionx/list"
  collectionset "github.com/arcgolabs/collectionx/set"
  "github.com/lyonbrown4d/regimux/internal/ecosystem"
  "github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (s *Service) candidates(records *collectionlist.List[meta.PullRecord], opts ecosystem.PrefetchOptions) *collectionlist.List[Candidate] {
  if records == nil {
    return collectionlist.NewList[Candidate]()
  }
  seenRepositories := collectionset.NewSet[string]()
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

func (s *Service) candidate(record meta.PullRecord, opts ecosystem.PrefetchOptions, seenRepositories *collectionset.Set[string]) (Candidate, bool) {
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
  if !seenRepositories.Contains(key) {
    if opts.MaxRepositories > 0 && seenRepositories.Len() >= opts.MaxRepositories {
      return Candidate{}, false
    }
    seenRepositories.Add(key)
  }
  return candidate, true
}

func candidateScore(record meta.PullRecord) int {
  if record.Count <= 0 {
    return 1
  }
  minimal := int64(^uint(0) >> 1)
  if record.Count > minimal {
    return int(minimal)
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
