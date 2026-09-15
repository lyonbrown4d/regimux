package meta

func (m *MetadataMapper) EndpointHealthRecordToRow(record EndpointHealthRecord) (endpointHealthRow, error) {
	return m.mapMetadata[endpointHealthRow](record)
}

func (m *MetadataMapper) EndpointHealthRowToRecord(row endpointHealthRow) (*EndpointHealthRecord, error) {
	record, err := m.mapMetadata[EndpointHealthRecord](row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (m *MetadataMapper) UpstreamRecordToRow(record Upstream) (upstreamRow, error) {
	return m.mapMetadata[upstreamRow](record)
}

func (m *MetadataMapper) UpstreamRowToRecord(row upstreamRow) (*Upstream, error) {
	record, err := m.mapMetadata[Upstream](row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (m *MetadataMapper) RepositoryRecordToRow(record Repository) (repositoryRow, error) {
	row, err := m.mapMetadata[repositoryRow](record)
	if err != nil {
		return repositoryRow{}, err
	}
	row.Key = repositoryMetadataKey(record.Alias, record.Name)
	return row, nil
}

func (m *MetadataMapper) RepositoryRowToRecord(row repositoryRow) (*Repository, error) {
	record, err := m.mapMetadata[Repository](row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}
