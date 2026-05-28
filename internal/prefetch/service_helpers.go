package prefetch

import (
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
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

func filterPullRecords(records *collectionlist.List[meta.PullRecord], opts RunOptions) *collectionlist.List[meta.PullRecord] {
	out := collectionlist.FilterList(records, func(_ int, record meta.PullRecord) bool {
		return record.Count >= opts.MinPullCount && record.Reference != ""
	}).Sort(comparePullRecordPriority)
	if out.Len() > opts.MaxRecords {
		return out.Take(opts.MaxRecords)
	}
	return out
}

func comparePullRecordPriority(left, right meta.PullRecord) int {
	switch {
	case left.LastPullAt.After(right.LastPullAt):
		return -1
	case left.LastPullAt.Before(right.LastPullAt):
		return 1
	case left.Count > right.Count:
		return -1
	case left.Count < right.Count:
		return 1
	default:
		return 0
	}
}

func groupPullRecords(records *collectionlist.List[meta.PullRecord]) *collectionmapping.MultiMap[repoKey, meta.PullRecord] {
	return collectionmapping.GroupByList(records, func(_ int, record meta.PullRecord) repoKey {
		return repoKey{alias: record.Alias, repo: record.Repository}
	})
}

func toCandidateRecords(records *collectionlist.List[meta.PullRecord]) *collectionlist.List[PullRecord] {
	return collectionlist.MapList(records, func(_ int, record meta.PullRecord) PullRecord {
		count := min(record.Count, int64(^uint(0)>>1))
		return PullRecord{
			Alias:      record.Alias,
			Repo:       record.Repository,
			Tag:        record.Reference,
			Count:      int(count),
			LastPullAt: record.LastPullAt,
		}
	})
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
