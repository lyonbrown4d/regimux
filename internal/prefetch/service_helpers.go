package prefetch

import (
	"sort"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/oops"
)

func normalizeRunOptions(opts RunOptions) RunOptions {
	if opts.MaxRecords <= 0 {
		opts.MaxRecords = defaultMaxRecords
	}
	if opts.TagsPageSize <= 0 {
		opts.TagsPageSize = defaultTagsPageSize
	}
	if opts.MinPullCount <= 0 {
		opts.MinPullCount = defaultMinPullCount
	}
	if opts.MaxCandidatesPerRepo <= 0 {
		opts.MaxCandidatesPerRepo = defaultMaxCandidates
	}
	if opts.MaxVersionDistance <= 0 {
		opts.MaxVersionDistance = defaultMaxVersionDistance
	}
	if opts.Accept == "" {
		opts.Accept = distribution.DefaultManifestAccept
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	return opts
}

func filterPullRecords(records []meta.PullRecord, opts RunOptions) []meta.PullRecord {
	out := make([]meta.PullRecord, 0, len(records))
	for i := range records {
		if records[i].Count < opts.MinPullCount || records[i].Reference == "" {
			continue
		}
		out = append(out, records[i])
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastPullAt.Equal(out[j].LastPullAt) {
			return out[i].LastPullAt.After(out[j].LastPullAt)
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > opts.MaxRecords {
		out = out[:opts.MaxRecords]
	}
	return out
}

func groupPullRecords(records []meta.PullRecord) map[repoKey][]meta.PullRecord {
	groups := make(map[repoKey][]meta.PullRecord)
	for i := range records {
		key := repoKey{alias: records[i].Alias, repo: records[i].Repository}
		groups[key] = append(groups[key], records[i])
	}
	return groups
}

func toCandidateRecords(records []meta.PullRecord) []PullRecord {
	out := make([]PullRecord, 0, len(records))
	for i := range records {
		record := records[i]
		count := min(record.Count, int64(^uint(0)>>1))
		out = append(out, PullRecord{
			Alias:      record.Alias,
			Repo:       record.Repository,
			Tag:        record.Reference,
			Count:      int(count),
			LastPullAt: record.LastPullAt,
		})
	}
	return out
}

func cacheError(message string) error {
	return oops.In("prefetch").Errorf("%s", message)
}

func cacheWrap(err error, message string) error {
	if err == nil {
		return nil
	}
	return oops.In("prefetch").Wrapf(err, "%s", message)
}
