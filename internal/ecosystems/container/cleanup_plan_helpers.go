package container

import (
	"sort"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func ownedCleanupManifests(groups []ecosystem.CacheCleanupGroup) map[string]struct{} {
	keys := make(map[string]struct{})
	for i := range groups {
		for j := range groups[i].Manifests {
			keys[cleanupManifestKey(groups[i].Manifests[j])] = struct{}{}
		}
	}
	return keys
}

func repoDigest(alias, repository, digest string) string {
	return alias + "\x00" + repository + "\x00" + digest
}

func splitRepoDigestKey(key string) (string, string, string) {
	parts := strings.SplitN(key, "\x00", 3)
	if len(parts) != 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

func refKey(alias, repository, reference string) string {
	return alias + "\x00" + repository + "\x00" + reference
}

func latest(current time.Time, candidates ...time.Time) time.Time {
	for _, candidate := range candidates {
		if candidate.After(current) {
			current = candidate
		}
	}
	return current
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
