package admin

func (s *Service) activitySummary(snapshot metadataSnapshot) (ActivitySummary, error) {
	rows, err := s.mapper.ActivityRowsFromPulls(snapshot.pulls, 50)
	if err != nil {
		return ActivitySummary{}, err
	}
	return ActivitySummary{
		RequestAuditAvailable: false,
		Rows:                  rows,
	}, nil
}
