package meta

import (
	"fmt"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/reference"
)

func normalizeManifestRecord(record ManifestRecord) (ManifestKey, ManifestRecord, error) {
	key, err := normalizeManifestKey(ManifestKey{
		Alias:      record.Alias,
		Repository: record.Repository,
		Digest:     record.Digest,
	})
	if err != nil {
		return ManifestKey{}, ManifestRecord{}, err
	}
	if record.Size < 0 {
		return ManifestKey{}, ManifestRecord{}, fmt.Errorf("%w: manifest size cannot be negative", ErrInvalidValue)
	}
	record.Alias = key.Alias
	record.Repository = key.Repository
	record.Digest = key.Digest
	record.Key = key.String()
	record.Headers = cloneHeaders(record.Headers)
	return key, record, nil
}

func normalizeManifestKey(key ManifestKey) (ManifestKey, error) {
	alias, err := required("alias", key.Alias)
	if err != nil {
		return ManifestKey{}, err
	}
	repo, err := required("repository", key.Repository)
	if err != nil {
		return ManifestKey{}, err
	}
	digest, err := normalizeDigest(key.Digest)
	if err != nil {
		return ManifestKey{}, err
	}
	return ManifestKey{Alias: alias, Repository: repo, Digest: digest}, nil
}

func normalizeTagRecord(record TagRecord) (TagKey, TagRecord, error) {
	key, err := normalizeTagKey(TagKey{
		Alias:      record.Alias,
		Repository: record.Repository,
		Reference:  record.Reference,
	})
	if err != nil {
		return TagKey{}, TagRecord{}, err
	}
	digest, err := normalizeDigest(record.Digest)
	if err != nil {
		return TagKey{}, TagRecord{}, err
	}
	record.Alias = key.Alias
	record.Repository = key.Repository
	record.Reference = key.Reference
	record.Digest = digest
	record.Key = key.String()
	return key, record, nil
}

func normalizeTagKey(key TagKey) (TagKey, error) {
	alias, err := required("alias", key.Alias)
	if err != nil {
		return TagKey{}, err
	}
	repo, err := required("repository", key.Repository)
	if err != nil {
		return TagKey{}, err
	}
	ref, err := required("reference", key.Reference)
	if err != nil {
		return TagKey{}, err
	}
	return TagKey{Alias: alias, Repository: repo, Reference: ref}, nil
}

func normalizeBlobRecord(record BlobRecord) (BlobKey, BlobRecord, error) {
	key, err := normalizeBlobKey(BlobKey{Digest: record.Digest})
	if err != nil {
		return BlobKey{}, BlobRecord{}, err
	}
	if record.Size < 0 {
		return BlobKey{}, BlobRecord{}, fmt.Errorf("%w: blob size cannot be negative", ErrInvalidValue)
	}
	record.Digest = key.Digest
	return key, record, nil
}

func normalizeRepoBlobRecord(record RepoBlobRecord) (RepoBlobKey, RepoBlobRecord, error) {
	key, err := normalizeRepoBlobKey(RepoBlobKey{
		Alias:      record.Alias,
		Repository: record.Repository,
		Digest:     record.Digest,
	})
	if err != nil {
		return RepoBlobKey{}, RepoBlobRecord{}, err
	}
	record.Alias = key.Alias
	record.Repository = key.Repository
	record.Digest = key.Digest
	record.Key = key.String()
	return key, record, nil
}

func normalizeRepoBlobKey(key RepoBlobKey) (RepoBlobKey, error) {
	alias, err := required("alias", key.Alias)
	if err != nil {
		return RepoBlobKey{}, err
	}
	repo, err := required("repository", key.Repository)
	if err != nil {
		return RepoBlobKey{}, err
	}
	digest, err := normalizeDigest(key.Digest)
	if err != nil {
		return RepoBlobKey{}, err
	}
	return RepoBlobKey{Alias: alias, Repository: repo, Digest: digest}, nil
}

func normalizeBlobKey(key BlobKey) (BlobKey, error) {
	digest, err := normalizeDigest(key.Digest)
	if err != nil {
		return BlobKey{}, err
	}
	return BlobKey{Digest: digest}, nil
}

func required(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: %s is required", ErrInvalidKey, name)
	}
	return value, nil
}

func normalizeDigest(value string) (string, error) {
	digest, err := reference.NormalizeDigest(value)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	return digest, nil
}

func cloneHeaders(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		out[key] = append([]string(nil), values...)
	}
	return out
}
