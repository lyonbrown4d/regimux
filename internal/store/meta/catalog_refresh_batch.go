package meta

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/go-singleflightx"
	"golang.org/x/sync/semaphore"
)

type catalogRefreshKey struct {
	Alias  string
	Digest string
}

type catalogRefreshState struct {
	revision            uint64
	at                  time.Time
	pendingRepositories map[string]time.Time
	users               int
	callbacks           int
	batchGate           *semaphore.Weighted
	group               singleflightx.Group[struct{}, uint64]
}

func (s *SQLStore) refreshRepositoryMetadata(ctx context.Context, alias, name string, at time.Time) error {
	return s.refreshRepositoryMetadataBatch(ctx, alias, map[string]time.Time{name: at})
}

func (s *SQLStore) refreshRepositoryMetadataBatch(ctx context.Context, alias string, updates map[string]time.Time) error {
	if len(updates) == 0 {
		return nil
	}
	alias, updates, err := normalizeRepositoryCatalogUpdates(alias, updates)
	if err != nil {
		return err
	}
	key := catalogRefreshKey{Alias: alias}
	state, ticket := s.beginRepositoryCatalogRefresh(alias, updates)
	defer s.endCatalogRefresh(key, state)
	return s.waitRepositoryCatalogRefresh(ctx, alias, state, ticket)
}

func normalizeRepositoryCatalogUpdates(alias string, updates map[string]time.Time) (string, map[string]time.Time, error) {
	alias, err := normalizeUpstreamAlias(alias)
	if err != nil {
		return "", nil, err
	}
	normalized := make(map[string]time.Time, len(updates))
	for name, at := range updates {
		normalizedName, err := normalizeRepositoryName(name)
		if err != nil {
			return "", nil, err
		}
		normalized[normalizedName] = maxTime(normalized[normalizedName], metadataTimestamp(at))
	}
	return alias, normalized, nil
}

func (s *SQLStore) waitRepositoryCatalogRefresh(ctx context.Context, alias string, state *catalogRefreshState, ticket uint64) error {
	for {
		covered, err, _ := state.group.DoContext(ctx, struct{}{}, func(sharedCtx context.Context) (uint64, error) {
			return s.runRepositoryCatalogRefresh(sharedCtx, alias, state)
		})
		done, resultErr := repositoryCatalogBatchResult(ctx, ticket, covered, err)
		if done {
			return resultErr
		}
		// A later committed mutation stays queued for the next alias batch.
	}
}

func repositoryCatalogBatchResult(ctx context.Context, ticket, covered uint64, batchErr error) (bool, error) {
	if covered >= ticket && batchErr != nil {
		return true, wrapError(batchErr, "refresh repository catalog")
	}
	if covered >= ticket {
		return true, nil
	}
	if batchErr != nil && ctx.Err() != nil {
		return true, wrapError(batchErr, "refresh repository catalog")
	}
	if err := ctx.Err(); err != nil {
		return true, fmt.Errorf("refresh repository catalog: %w", err)
	}
	return false, nil
}

func (s *SQLStore) runRepositoryCatalogRefresh(ctx context.Context, alias string, state *catalogRefreshState) (uint64, error) {
	s.beginCatalogRefreshCallback(state)
	defer s.endCatalogRefreshCallback(catalogRefreshKey{Alias: alias}, state)

	release, err := s.acquireCatalogRefresh(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	if gateErr := state.batchGate.Acquire(ctx, 1); gateErr != nil {
		return 0, wrapError(gateErr, "wait for alias catalog refresh")
	}
	defer state.batchGate.Release(1)
	if contextErr := ctx.Err(); contextErr != nil {
		return 0, wrapError(contextErr, "check alias catalog refresh context")
	}
	covered, pending := s.catalogRepositoryRefreshSnapshot(state)
	if len(pending) == 0 {
		return covered, nil
	}
	refreshErr := s.refreshRepositoryMetadataBatchOnce(ctx, alias, pending)
	if refreshErr == nil {
		return covered, nil
	}
	s.requeueCatalogRepositoryRefresh(state, pending)
	if ctx.Err() != nil {
		return covered, refreshErr
	}
	firstErr := refreshErr
	covered, pending = s.catalogRepositoryRefreshSnapshot(state)
	if len(pending) == 0 {
		return covered, firstErr
	}
	if retryErr := s.refreshRepositoryMetadataBatchOnce(ctx, alias, pending); retryErr != nil {
		s.requeueCatalogRepositoryRefresh(state, pending)
		return covered, retryErr
	}
	return covered, nil
}

func (s *SQLStore) beginCatalogRefreshCallback(state *catalogRefreshState) {
	s.catalogRefreshMu.Lock()
	defer s.catalogRefreshMu.Unlock()
	state.callbacks++
}

func (s *SQLStore) endCatalogRefreshCallback(key catalogRefreshKey, state *catalogRefreshState) {
	s.catalogRefreshMu.Lock()
	defer s.catalogRefreshMu.Unlock()
	state.callbacks--
	if state.users == 0 && state.callbacks == 0 && s.catalogRefreshes[key] == state {
		delete(s.catalogRefreshes, key)
	}
}

func (s *SQLStore) requeueCatalogRepositoryRefresh(state *catalogRefreshState, updates map[string]time.Time) {
	s.catalogRefreshMu.Lock()
	defer s.catalogRefreshMu.Unlock()
	if state.pendingRepositories == nil {
		state.pendingRepositories = make(map[string]time.Time, len(updates))
	}
	for name, at := range updates {
		state.pendingRepositories[name] = maxTime(state.pendingRepositories[name], at)
	}
}

func (s *SQLStore) refreshCatalogMetadata(
	ctx context.Context,
	key catalogRefreshKey,
	at time.Time,
	refresh func(context.Context, time.Time) error,
) error {
	state, ticket := s.beginCatalogRefresh(key, at)
	defer s.endCatalogRefresh(key, state)
	for {
		covered, err, _ := state.group.DoContext(ctx, struct{}{}, func(sharedCtx context.Context) (uint64, error) {
			covered, latestAt := s.catalogRefreshSnapshot(state)
			return covered, refresh(sharedCtx, latestAt)
		})
		if err != nil {
			return wrapError(err, "run catalog refresh batch")
		}
		if covered >= ticket {
			return nil
		}
		// A later committed mutation needs another batch, including its timestamp.
	}
}

func (s *SQLStore) beginCatalogRefresh(key catalogRefreshKey, at time.Time) (*catalogRefreshState, uint64) {
	s.catalogRefreshMu.Lock()
	defer s.catalogRefreshMu.Unlock()
	if s.catalogRefreshes == nil {
		s.catalogRefreshes = make(map[catalogRefreshKey]*catalogRefreshState)
	}
	state := s.catalogRefreshes[key]
	if state == nil {
		state = newCatalogRefreshState()
		s.catalogRefreshes[key] = state
	}
	// Callers register only after their underlying metadata mutation commits.
	state.revision++
	state.users++
	state.at = maxTime(state.at, metadataTimestamp(at))
	return state, state.revision
}

func (s *SQLStore) beginRepositoryCatalogRefresh(alias string, updates map[string]time.Time) (*catalogRefreshState, uint64) {
	s.catalogRefreshMu.Lock()
	defer s.catalogRefreshMu.Unlock()
	if s.catalogRefreshes == nil {
		s.catalogRefreshes = make(map[catalogRefreshKey]*catalogRefreshState)
	}
	key := catalogRefreshKey{Alias: alias}
	state := s.catalogRefreshes[key]
	if state == nil {
		state = newCatalogRefreshState()
		s.catalogRefreshes[key] = state
	}
	if state.pendingRepositories == nil {
		state.pendingRepositories = make(map[string]time.Time, len(updates))
	}
	state.revision++
	state.users++
	for name, at := range updates {
		state.pendingRepositories[name] = maxTime(state.pendingRepositories[name], metadataTimestamp(at))
	}
	return state, state.revision
}

func newCatalogRefreshState() *catalogRefreshState {
	return &catalogRefreshState{batchGate: semaphore.NewWeighted(1)}
}

func (s *SQLStore) catalogRepositoryRefreshSnapshot(state *catalogRefreshState) (uint64, map[string]time.Time) {
	s.catalogRefreshMu.Lock()
	defer s.catalogRefreshMu.Unlock()
	pending := state.pendingRepositories
	state.pendingRepositories = nil
	return state.revision, pending
}

func (s *SQLStore) catalogRefreshSnapshot(state *catalogRefreshState) (uint64, time.Time) {
	s.catalogRefreshMu.Lock()
	defer s.catalogRefreshMu.Unlock()
	return state.revision, state.at
}

func (s *SQLStore) endCatalogRefresh(key catalogRefreshKey, state *catalogRefreshState) {
	s.catalogRefreshMu.Lock()
	defer s.catalogRefreshMu.Unlock()
	state.users--
	if state.users == 0 && state.callbacks == 0 && s.catalogRefreshes[key] == state {
		// Retain state only while callers are active, rather than for every repository.
		delete(s.catalogRefreshes, key)
	}
}
