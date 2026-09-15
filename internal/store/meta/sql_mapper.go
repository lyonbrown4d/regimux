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

func (m *MetadataMapper) mapMetadata[D, S any](src S) (D, error) {
	if m == nil || m.mapper == nil {
		m = NewMetadataMapper()
	}
	dst, err := m.mapper.Map[D](src)
	if err != nil {
		return dst, wrapError(err, "map metadata row")
	}
	return dst, nil
}

func (m *MetadataMapper) ManifestRecordToRow(record ManifestRecord) (manifestRow, error) {
	return m.mapMetadata[manifestRow](record)
}

func (m *MetadataMapper) ManifestRowToRecord(row manifestRow) (*ManifestRecord, error) {
	record, err := m.mapMetadata[ManifestRecord](row)
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
