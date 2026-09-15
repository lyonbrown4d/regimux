package meta

func (m *MetadataMapper) PrefetchControlRecordToRow(record PrefetchControlRecord) (prefetchControlRow, error) {
	return m.mapMetadata[prefetchControlRow](record)
}

func (m *MetadataMapper) PrefetchControlRowToRecord(row prefetchControlRow) (*PrefetchControlRecord, error) {
	record, err := m.mapMetadata[PrefetchControlRecord](row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}
