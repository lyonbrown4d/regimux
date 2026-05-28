package meta

import (
	"time"

	mapperx "github.com/arcgolabs/mapper"
)

type MetadataMapper struct {
	mapper *mapperx.Mapper
}

func NewMetadataMapper() *MetadataMapper {
	return &MetadataMapper{
		mapper: mapperx.New(
			mapperx.Converter(unixNano),
			mapperx.Converter(timeFromUnixNano),
			mapperx.Converter(durationNanos),
			mapperx.Converter(durationFromInt64),
			mapperx.Converter(int64FromInt),
			mapperx.Converter(intFromInt64),
			mapperx.ConverterE(encodeHeaders),
			mapperx.ConverterE(decodeHeaders),
		),
	}
}

func mapMetadata[D any](m *MetadataMapper, src any) (D, error) {
	var dst D
	if m == nil || m.mapper == nil {
		m = NewMetadataMapper()
	}
	if err := m.mapper.MapInto(&dst, src); err != nil {
		return dst, wrapError(err, "map metadata row")
	}
	return dst, nil
}

func (m *MetadataMapper) ManifestRecordToRow(record ManifestRecord) (manifestRow, error) {
	return mapMetadata[manifestRow](m, record)
}

func (m *MetadataMapper) ManifestRowToRecord(row manifestRow) (*ManifestRecord, error) {
	record, err := mapMetadata[ManifestRecord](m, row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func durationNanos(value time.Duration) int64 {
	return int64(value)
}

func int64FromInt(value int) int64 {
	return int64(value)
}
