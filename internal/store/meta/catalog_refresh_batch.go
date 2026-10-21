package meta

import (
	"context"
	"time"

	"github.com/samber/go-singleflightx"
)

type catalogRefreshKey struct {
	Alias      string
	Repository string
}

type catalogRefreshState struct {
	revision uint64
	at       time.Time
	users    int
	group    singleflightx.Group[struct{}, uint64]
}

func (s *SQLStore) refreshRepositoryMetadata(ctx context.Context, alias, name string, at time.Time) error {
	key := catalogRefreshKey{Alias: alias, Repository: name}
	state, ticket := s.beginCatalogRefresh(key, at)
	defer s.endCatalogRefresh(key, state)
	for {
		covered, err, _ := state.group.DoContext(ctx, struct{}{}, func(sharedCtx context.Context) (uint64, error) {
			covered, latestAt := s.catalogRefreshSnapshot(state)
			return covered, s.refreshRepositoryMetadataOnce(sharedCtx, alias, name, latestAt)
		})
		if err != nil {
			return wrapError(err, "refresh repository catalog")
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
		state = &catalogRefreshState{}
		s.catalogRefreshes[key] = state
	}
	// Callers register only after their underlying metadata mutation commits.
	state.revision++
	state.users++
	state.at = maxTime(state.at, metadataTimestamp(at))
	return state, state.revision
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
	if state.users == 0 && s.catalogRefreshes[key] == state {
		// Retain state only while callers are active, rather than for every repository.
		delete(s.catalogRefreshes, key)
	}
}
