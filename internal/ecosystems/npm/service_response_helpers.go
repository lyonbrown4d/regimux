package npm

import "context"

func (s *Service) responseFromCached(req Request, requestRoute route, cached storedResponse, cachedOK bool, mode requestMode) (*Response, bool, error) {
	if mode == requestModeRefresh || !cachedOK {
		return nil, false, nil
	}
	if cached.expired {
		resp, err := s.responseFromStored(req, requestRoute, cached, cacheStale)
		return resp, true, err
	}
	resp, err := s.responseFromStored(req, requestRoute, cached, cacheHit)
	return resp, true, err
}

func (s *Service) responseFromFetched(ctx context.Context, req Request, requestRoute route, fetched *upstreamFetch) (*Response, error) {
	if shouldPassThrough(req, fetched.status) || !cacheable(requestRoute) {
		return s.responseFromUpstream(req, requestRoute, fetched), nil
	}
	prepared, err := s.prepareFetched(req, requestRoute, fetched)
	if err != nil {
		return nil, err
	}
	return s.storeFetchedResponse(ctx, req, requestRoute, prepared)
}
