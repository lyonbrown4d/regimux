package meta

func (m *MetadataMapper) PrefetchControlRecordToRow(record PrefetchControlRecord) (prefetchControlRow, error) {
	return mapMetadata[prefetchControlRow](m, record)
}

func (m *MetadataMapper) PrefetchControlRowToRecord(row prefetchControlRow) (*PrefetchControlRecord, error) {
	record, err := mapMetadata[PrefetchControlRecord](m, row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}
