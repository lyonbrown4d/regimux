package meta

func (m *MetadataMapper) EndpointHealthRecordToRow(record EndpointHealthRecord) (endpointHealthRow, error) {
	return mapMetadata[endpointHealthRow](m, record)
}

func (m *MetadataMapper) EndpointHealthRowToRecord(row endpointHealthRow) (*EndpointHealthRecord, error) {
	record, err := mapMetadata[EndpointHealthRecord](m, row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (m *MetadataMapper) UpstreamRecordToRow(record Upstream) (upstreamRow, error) {
	return mapMetadata[upstreamRow](m, record)
}

func (m *MetadataMapper) UpstreamRowToRecord(row upstreamRow) (*Upstream, error) {
	record, err := mapMetadata[Upstream](m, row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (m *MetadataMapper) RepositoryRecordToRow(record Repository) (repositoryRow, error) {
	row, err := mapMetadata[repositoryRow](m, record)
	if err != nil {
		return repositoryRow{}, err
	}
	row.Key = repositoryMetadataKey(record.Alias, record.Name)
	return row, nil
}

func (m *MetadataMapper) RepositoryRowToRecord(row repositoryRow) (*Repository, error) {
	record, err := mapMetadata[Repository](m, row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}
