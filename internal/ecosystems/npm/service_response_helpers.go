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
	var release func()
	if requestRoute.Kind == routeMetadata {
		var err error
		release, err = acquireMetadataSlot(ctx, s.metadataSlots)
		if err != nil {
			closeReadCloser(fetched.body, s.logger, "close npm metadata after admission cancellation")
			return nil, wrapError(err, "admit npm metadata transformation")
		}
		defer func() {
			if release != nil {
				release()
			}
		}()
	}
	prepared, err := s.prepareFetched(req, requestRoute, fetched)
	if err != nil {
		return nil, err
	}
	if release != nil {
		release()
		release = nil
	}
	return s.storeFetchedResponse(ctx, req, requestRoute, prepared)
}
