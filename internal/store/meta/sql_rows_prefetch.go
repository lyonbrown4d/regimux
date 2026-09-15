package meta

func (m *MetadataMapper) PrefetchRunRecordToRow(record PrefetchRunRecord) (prefetchRunRow, error) {
	return m.mapMetadata[prefetchRunRow](record)
}

func (m *MetadataMapper) PrefetchRunRowToRecord(row prefetchRunRow) (*PrefetchRunRecord, error) {
	record, err := m.mapMetadata[PrefetchRunRecord](row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (m *MetadataMapper) PrefetchOutcomeRecordToRow(record PrefetchOutcomeRecord) (prefetchOutcomeRow, error) {
	return m.mapMetadata[prefetchOutcomeRow](record)
}

func (m *MetadataMapper) PrefetchOutcomeRowToRecord(row prefetchOutcomeRow) (*PrefetchOutcomeRecord, error) {
	record, err := m.mapMetadata[PrefetchOutcomeRecord](row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}
