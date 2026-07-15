package maven

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/config"
)

type groupFetchState struct {
	miss      *upstreamFetch
	failure   *upstreamFetch
	lastError error
}

func (s *Service) fetch(
	ctx context.Context,
	upstream config.UpstreamConfig,
	upstreamAlias string,
	route Route,
	method string,
) (*upstreamFetch, error) {
	group, grouped := s.group(upstreamAlias)
	if grouped {
		return s.fetchGroup(ctx, group, route, method)
	}
	fetched, err := s.fetchPhysical(ctx, upstream, upstreamAlias, route, method)
	if err == nil {
		setResolvedUpstream(fetched, upstreamAlias)
	}
	return fetched, err
}

func (s *Service) fetchGroup(
	ctx context.Context,
	group config.MavenGroupConfig,
	route Route,
	method string,
) (*upstreamFetch, error) {
	if shouldMergeGroupMetadata(group, route) {
		return s.fetchMergedGroupMetadata(ctx, group, route, method)
	}
	return s.fetchFirstGroupMember(ctx, group, route, method)
}

func (s *Service) fetchFirstGroupMember(
	ctx context.Context,
	group config.MavenGroupConfig,
	route Route,
	method string,
) (*upstreamFetch, error) {
	state := groupFetchState{}
	for _, member := range group.Members {
		fetched, err := s.fetchGroupMember(ctx, member, route, method)
		if err != nil {
			if rememberErr := state.rememberError(err, group.FallbackOnError); rememberErr != nil {
				state.close(s)
				return nil, rememberErr
			}
			continue
		}

		switch classifyGroupResponse(fetched.status, group.FallbackOnError) {
		case groupResponseMiss:
			state.rememberMiss(s, fetched)
		case groupResponseFallback:
			state.rememberFailure(s, fetched)
		case groupResponseTerminal:
			state.close(s)
			return fetched, nil
		}
	}
	return state.result(s, route.Alias)
}

func (s *Service) fetchGroupMember(
	ctx context.Context,
	member string,
	route Route,
	method string,
) (*upstreamFetch, error) {
	memberRoute := route
	memberRoute.Alias = member
	if err := s.checkDependencyPolicy(memberRoute); err != nil {
		s.recordPolicyDeniedPull(ctx, Request{
			Alias:  member,
			Tail:   route.Tail,
			Method: method,
		}, memberRoute, err)
		return nil, err
	}
	upstream, ok := s.cfg.MavenUpstream(member)
	if !ok {
		return nil, fmt.Errorf("maven group %q references unavailable member %q", route.Alias, member)
	}
	fetched, err := s.fetchPhysical(ctx, upstream, member, route, method)
	if err == nil {
		setResolvedUpstream(fetched, member)
	}
	return fetched, err
}

type groupResponseDisposition uint8

const (
	groupResponseTerminal groupResponseDisposition = iota
	groupResponseMiss
	groupResponseFallback
)

func classifyGroupResponse(status int, fallbackOnError bool) groupResponseDisposition {
	if status == http.StatusNotFound || status == http.StatusGone {
		return groupResponseMiss
	}
	if fallbackOnError && status >= http.StatusInternalServerError {
		return groupResponseFallback
	}
	return groupResponseTerminal
}

func shouldMergeGroupMetadata(group config.MavenGroupConfig, route Route) bool {
	policy := strings.ToLower(strings.TrimSpace(group.MetadataPolicy))
	if policy == "" {
		policy = config.MavenMetadataPolicyMerge
	}
	return policy == config.MavenMetadataPolicyMerge &&
		route.Kind == RouteMetadata &&
		!strings.Contains(strings.ToUpper(route.Tail), "SNAPSHOT")
}

func setResolvedUpstream(fetched *upstreamFetch, alias string) {
	if fetched == nil {
		return
	}
	if fetched.headers == nil {
		fetched.headers = make(http.Header)
	}
	fetched.headers.Set(resolvedUpstreamHeader, alias)
}

func (state *groupFetchState) rememberError(err error, fallbackOnError bool) error {
	if !fallbackOnError {
		return err
	}
	state.lastError = err
	return nil
}

func (state *groupFetchState) rememberMiss(s *Service, fetched *upstreamFetch) {
	s.discardGroupFetch(state.miss)
	state.miss = fetched
}

func (state *groupFetchState) rememberFailure(s *Service, fetched *upstreamFetch) {
	s.discardGroupFetch(state.failure)
	state.failure = fetched
}

func (state *groupFetchState) result(s *Service, alias string) (*upstreamFetch, error) {
	if state.lastError != nil {
		state.close(s)
		return nil, state.lastError
	}
	if state.failure != nil {
		s.discardGroupFetch(state.miss)
		return state.failure, nil
	}
	if state.miss != nil {
		return state.miss, nil
	}
	return nil, fmt.Errorf("maven group %q has no members", alias)
}

func (state *groupFetchState) close(s *Service) {
	s.discardGroupFetch(state.miss)
	s.discardGroupFetch(state.failure)
	state.miss = nil
	state.failure = nil
}

func (s *Service) discardGroupFetch(fetched *upstreamFetch) {
	if fetched == nil {
		return
	}
	closeReadCloser(fetched.body, s.logger, "close skipped Maven group response")
}
