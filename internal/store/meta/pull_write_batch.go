package meta

import (
	"context"

	"github.com/samber/go-singleflightx"
)

type pullWriteBatchState struct {
	pending   pullRow
	mutations []*pullWriteMutation
	users     int
	group     singleflightx.Group[struct{}, bool]
}

type pullWriteBatchResult struct {
	record PullRecord
	err    error
}

type pullWriteMutation struct {
	done    chan struct{}
	outcome *pullWriteBatchResult
}

func (s *SQLStore) recordPullBatch(ctx context.Context, key PullKey, row pullRow) (*PullRecord, error) {
	if err := requireMetadataContext(ctx, "record pull metadata"); err != nil {
		return nil, err
	}
	state, mutation := s.beginPullWriteBatch(key.String(), row)
	defer s.endPullWriteBatch(key.String(), state)
	for {
		if result := completedPullMutation(mutation); result != nil {
			if result.err != nil {
				return nil, wrapError(result.err, "write batched pull metadata")
			}
			return clonePullRecord(result.record), nil
		}
		_, err, _ := state.group.DoContext(ctx, struct{}{}, func(sharedCtx context.Context) (bool, error) {
			s.flushPullWriteBatch(sharedCtx, key, state, mutation)
			return true, nil
		})
		if err != nil {
			return nil, wrapError(err, "write batched pull metadata")
		}
	}
}

func completedPullMutation(mutation *pullWriteMutation) *pullWriteBatchResult {
	select {
	case <-mutation.done:
		return mutation.outcome
	default:
		return nil
	}
}

func (s *SQLStore) flushPullWriteBatch(ctx context.Context, key PullKey, state *pullWriteBatchState, own *pullWriteMutation) {
	row, mutations := s.snapshotPullWriteBatch(state)
	if len(mutations) == 0 {
		// A caller can miss a completed flight or a canceled callback still finishing.
		// Wait for its exact receipt; never flush an empty row or retry another batch.
		select {
		case <-own.done:
		case <-ctx.Done():
		}
		return
	}
	result := s.persistPullWriteBatch(ctx, key, row)
	for _, mutation := range mutations {
		mutation.outcome = &result
		close(mutation.done)
	}
}

func (s *SQLStore) persistPullWriteBatch(ctx context.Context, key PullKey, row pullRow) pullWriteBatchResult {
	if err := s.upsertPullRow(ctx, row); err != nil {
		return pullWriteBatchResult{err: err}
	}
	if err := s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, timeFromUnixNano(row.UpdatedAt)); err != nil {
		return pullWriteBatchResult{err: err}
	}
	record, ok, err := s.Pull(ctx, key)
	if err != nil {
		return pullWriteBatchResult{err: err}
	}
	if !ok {
		return pullWriteBatchResult{err: errorf("record pull metadata: %w", ErrNotFound)}
	}
	return pullWriteBatchResult{record: *record}
}

func (s *SQLStore) beginPullWriteBatch(key string, row pullRow) (*pullWriteBatchState, *pullWriteMutation) {
	s.pullWriteBatchMu.Lock()
	defer s.pullWriteBatchMu.Unlock()
	if s.pullWriteBatches == nil {
		s.pullWriteBatches = make(map[string]*pullWriteBatchState)
	}
	state := s.pullWriteBatches[key]
	if state == nil {
		state = &pullWriteBatchState{}
		s.pullWriteBatches[key] = state
	}
	state.users++
	mergePullWriteRows(&state.pending, row)
	mutation := &pullWriteMutation{done: make(chan struct{})}
	state.mutations = append(state.mutations, mutation)
	return state, mutation
}

func (s *SQLStore) snapshotPullWriteBatch(state *pullWriteBatchState) (pullRow, []*pullWriteMutation) {
	s.pullWriteBatchMu.Lock()
	defer s.pullWriteBatchMu.Unlock()
	row := state.pending
	mutations := state.mutations
	state.pending = pullRow{}
	state.mutations = nil
	return row, mutations
}

func (s *SQLStore) endPullWriteBatch(key string, state *pullWriteBatchState) {
	s.pullWriteBatchMu.Lock()
	defer s.pullWriteBatchMu.Unlock()
	state.users--
	if state.users == 0 && s.pullWriteBatches[key] == state {
		delete(s.pullWriteBatches, key)
	}
}

func mergePullWriteRows(target *pullRow, incoming pullRow) {
	if target.Key == "" {
		*target = incoming
		return
	}
	target.Count += incoming.Count
	target.PolicyDeniedCount += incoming.PolicyDeniedCount
	target.LastPullAt = max(target.LastPullAt, incoming.LastPullAt)
	target.LastUpstreamPullAt = max(target.LastUpstreamPullAt, incoming.LastUpstreamPullAt)
	target.LastPolicyDeniedAt = max(target.LastPolicyDeniedAt, incoming.LastPolicyDeniedAt)
	target.CreatedAt = minPullWriteTimestamp(target.CreatedAt, incoming.CreatedAt)
	target.UpdatedAt = max(target.UpdatedAt, incoming.UpdatedAt)
}

func clonePullRecord(record PullRecord) *PullRecord {
	return &record
}

func minPullWriteTimestamp(left, right int64) int64 {
	if left == 0 || (right != 0 && right < left) {
		return right
	}
	return left
}
