package meta

import (
	"encoding/json"
	"strings"
	"time"
)

func manifestRecordToRow(record ManifestRecord) (manifestRow, error) {
	return mapMetadata[manifestRow](record)
}

func manifestRowToRecord(row manifestRow) (*ManifestRecord, error) {
	record, err := mapMetadata[ManifestRecord](row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func tagRecordToRow(record TagRecord) tagRow {
	return tagRow{
		ID:         record.ID,
		Key:        record.Key,
		Alias:      record.Alias,
		Repository: record.Repository,
		Reference:  record.Reference,
		Digest:     record.Digest,
		ExpiresAt:  unixNano(record.ExpiresAt),
		CreatedAt:  unixNano(record.CreatedAt),
		UpdatedAt:  unixNano(record.UpdatedAt),
	}
}

func tagRowToRecord(row tagRow) *TagRecord {
	return &TagRecord{
		ID:         row.ID,
		Key:        row.Key,
		Alias:      row.Alias,
		Repository: row.Repository,
		Reference:  row.Reference,
		Digest:     row.Digest,
		ExpiresAt:  timeFromUnixNano(row.ExpiresAt),
		CreatedAt:  timeFromUnixNano(row.CreatedAt),
		UpdatedAt:  timeFromUnixNano(row.UpdatedAt),
	}
}

func pullRowToRecord(row pullRow) *PullRecord {
	return &PullRecord{
		ID:                 row.ID,
		Key:                row.Key,
		Alias:              row.Alias,
		Repository:         row.Repository,
		Reference:          row.Reference,
		Count:              row.Count,
		LastPullAt:         timeFromUnixNano(row.LastPullAt),
		LastUpstreamPullAt: timeFromUnixNano(row.LastUpstreamPullAt),
		CreatedAt:          timeFromUnixNano(row.CreatedAt),
		UpdatedAt:          timeFromUnixNano(row.UpdatedAt),
	}
}

func pullRecordToRow(record PullRecord) pullRow {
	return pullRow{
		ID:                 record.ID,
		Key:                record.Key,
		Alias:              record.Alias,
		Repository:         record.Repository,
		Reference:          record.Reference,
		Count:              record.Count,
		LastPullAt:         unixNano(record.LastPullAt),
		LastUpstreamPullAt: unixNano(record.LastUpstreamPullAt),
		CreatedAt:          unixNano(record.CreatedAt),
		UpdatedAt:          unixNano(record.UpdatedAt),
	}
}

func blobRecordToRow(record BlobRecord) blobRow {
	return blobRow{
		ID:           record.ID,
		Digest:       record.Digest,
		Size:         record.Size,
		MediaType:    record.MediaType,
		ObjectKey:    record.ObjectKey,
		CreatedAt:    unixNano(record.CreatedAt),
		UpdatedAt:    unixNano(record.UpdatedAt),
		LastAccessAt: unixNano(record.LastAccessAt),
	}
}

func blobRowToRecord(row blobRow) *BlobRecord {
	return &BlobRecord{
		ID:           row.ID,
		Digest:       row.Digest,
		Size:         row.Size,
		MediaType:    row.MediaType,
		ObjectKey:    row.ObjectKey,
		CreatedAt:    timeFromUnixNano(row.CreatedAt),
		UpdatedAt:    timeFromUnixNano(row.UpdatedAt),
		LastAccessAt: timeFromUnixNano(row.LastAccessAt),
	}
}

func repoBlobRecordToRow(record RepoBlobRecord) repoBlobRow {
	return repoBlobRow{
		ID:             record.ID,
		Key:            record.Key,
		Alias:          record.Alias,
		Repository:     record.Repository,
		Digest:         record.Digest,
		SourceManifest: record.SourceManifest,
		CreatedAt:      unixNano(record.CreatedAt),
		UpdatedAt:      unixNano(record.UpdatedAt),
		LastAccessAt:   unixNano(record.LastAccessAt),
		LastVerifiedAt: unixNano(record.LastVerifiedAt),
	}
}

func repoBlobRowToRecord(row repoBlobRow) *RepoBlobRecord {
	return &RepoBlobRecord{
		ID:             row.ID,
		Key:            row.Key,
		Alias:          row.Alias,
		Repository:     row.Repository,
		Digest:         row.Digest,
		SourceManifest: row.SourceManifest,
		CreatedAt:      timeFromUnixNano(row.CreatedAt),
		UpdatedAt:      timeFromUnixNano(row.UpdatedAt),
		LastAccessAt:   timeFromUnixNano(row.LastAccessAt),
		LastVerifiedAt: timeFromUnixNano(row.LastVerifiedAt),
	}
}

func encodeHeaders(headers map[string][]string) (string, error) {
	headers = cloneHeaders(headers)
	if len(headers) == 0 {
		return "", nil
	}
	data, err := json.Marshal(headers)
	if err != nil {
		return "", wrapError(err, "encode manifest headers")
	}
	return string(data), nil
}

func decodeHeaders(value string) (map[string][]string, error) {
	if strings.TrimSpace(value) == "" {
		return map[string][]string{}, nil
	}
	var headers map[string][]string
	if err := json.Unmarshal([]byte(value), &headers); err != nil {
		return nil, wrapError(err, "decode manifest headers")
	}
	return cloneHeaders(headers), nil
}

func durationFromInt64(value int64) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value)
}

func intFromInt64(value int64) int {
	if value <= 0 {
		return 0
	}
	maxInt := int64(^uint(0) >> 1)
	if value > maxInt {
		return int(maxInt)
	}
	return int(value)
}
