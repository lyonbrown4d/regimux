package meta

func (m *MetadataMapper) RefreshIntentRecordToRow(record RefreshIntentRecord) (refreshIntentRow, error) {
	return mapMetadata[refreshIntentRow](m, record)
}

func (m *MetadataMapper) RefreshIntentRowToRecord(row refreshIntentRow) (*RefreshIntentRecord, error) {
	record, err := mapMetadata[RefreshIntentRecord](m, row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}
