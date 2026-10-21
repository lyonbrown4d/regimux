package meta

import "testing"

func TestPullWriteBatchUsesOneReceiptPerPendingGeneration(t *testing.T) {
	store := &SQLStore{}
	key := PullKey{Alias: "npm/default", Repository: "demo", Reference: "1.0"}.String()
	row := pullRow{Key: key, Count: 1}
	state, first := store.beginPullWriteBatch(key, row)
	defer store.endPullWriteBatch(key, state)
	for range 512 {
		joinedState, receipt := store.beginPullWriteBatch(key, row)
		if receipt != first {
			t.Error("pending callers allocated separate receipts for the same durable batch")
		}
		store.endPullWriteBatch(key, joinedState)
	}
	if state.pending.Count != 513 {
		t.Fatalf("batched counter = %d, want 513", state.pending.Count)
	}
}
