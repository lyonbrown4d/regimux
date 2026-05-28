package meta

func (m *MetadataMapper) PrefetchRunRecordToRow(record PrefetchRunRecord) (prefetchRunRow, error) {
	return mapMetadata[prefetchRunRow](m, record)
}

func (m *MetadataMapper) PrefetchRunRowToRecord(row prefetchRunRow) (*PrefetchRunRecord, error) {
	record, err := mapMetadata[PrefetchRunRecord](m, row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (m *MetadataMapper) PrefetchOutcomeRecordToRow(record PrefetchOutcomeRecord) (prefetchOutcomeRow, error) {
	return mapMetadata[prefetchOutcomeRow](m, record)
}

func (m *MetadataMapper) PrefetchOutcomeRowToRecord(row prefetchOutcomeRow) (*PrefetchOutcomeRecord, error) {
	record, err := mapMetadata[PrefetchOutcomeRecord](m, row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}
