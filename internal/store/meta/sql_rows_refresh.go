package meta

func (m *MetadataMapper) RefreshIntentRecordToRow(record RefreshIntentRecord) (refreshIntentRow, error) {
	return m.mapMetadata[refreshIntentRow](record)
}

func (m *MetadataMapper) RefreshIntentRowToRecord(row refreshIntentRow) (*RefreshIntentRecord, error) {
	record, err := m.mapMetadata[RefreshIntentRecord](row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}
