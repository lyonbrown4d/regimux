package meta

func prefetchControlRecordToRow(record PrefetchControlRecord) prefetchControlRow {
	return prefetchControlRow{
		ID:          record.ID,
		Action:      record.Action,
		Reason:      record.Reason,
		RequestedAt: unixNano(record.RequestedAt),
		ConsumedAt:  unixNano(record.ConsumedAt),
		CreatedAt:   unixNano(record.CreatedAt),
		UpdatedAt:   unixNano(record.UpdatedAt),
	}
}

func prefetchControlRowToRecord(row prefetchControlRow) *PrefetchControlRecord {
	return &PrefetchControlRecord{
		ID:          row.ID,
		Action:      row.Action,
		Reason:      row.Reason,
		RequestedAt: timeFromUnixNano(row.RequestedAt),
		ConsumedAt:  timeFromUnixNano(row.ConsumedAt),
		CreatedAt:   timeFromUnixNano(row.CreatedAt),
		UpdatedAt:   timeFromUnixNano(row.UpdatedAt),
	}
}
