package meta

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"
)

func (m *MetadataMapper) TagRecordToRow(record TagRecord) (tagRow, error) {
	return mapMetadata[tagRow](m, record)
}

func (m *MetadataMapper) TagRowToRecord(row tagRow) (*TagRecord, error) {
	record, err := mapMetadata[TagRecord](m, row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (m *MetadataMapper) PullRecordToRow(record PullRecord) (pullRow, error) {
	return mapMetadata[pullRow](m, record)
}

func (m *MetadataMapper) PullRowToRecord(row pullRow) (*PullRecord, error) {
	record, err := mapMetadata[PullRecord](m, row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (m *MetadataMapper) BlobRecordToRow(record BlobRecord) (blobRow, error) {
	return mapMetadata[blobRow](m, record)
}

func (m *MetadataMapper) BlobRowToRecord(row blobRow) (*BlobRecord, error) {
	record, err := mapMetadata[BlobRecord](m, row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (m *MetadataMapper) RepoBlobRecordToRow(record RepoBlobRecord) (repoBlobRow, error) {
	return mapMetadata[repoBlobRow](m, record)
}

func (m *MetadataMapper) RepoBlobRowToRecord(row repoBlobRow) (*RepoBlobRecord, error) {
	record, err := mapMetadata[RepoBlobRecord](m, row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

type rowValues[T any] interface {
	Values() []T
}

func mapRows[T, R any](rows rowValues[T], mapper func(T) (*R, error)) ([]R, error) {
	records, err := lo.MapErr(rows.Values(), func(row T, _ int) (R, error) {
		record, err := mapper(row)
		if err != nil {
			var zero R
			return zero, err
		}
		return *record, nil
	})
	if err != nil {
		return nil, wrapError(err, "map metadata rows")
	}
	return records, nil
}

func firstRow[T any](rows rowValues[T]) mo.Option[T] {
	values := rows.Values()
	if len(values) == 0 {
		return mo.None[T]()
	}
	return mo.Some(values[0])
}

func encodeHeaders(headers map[string][]string) (string, error) {
	headers = NormalizeManifestHeaders(headers)
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
	return NormalizeManifestHeaders(headers), nil
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
