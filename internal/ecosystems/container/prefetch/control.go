package prefetch

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (s *Service) CancelPrefetch(ctx context.Context) (*ecosystem.PrefetchControlReport, error) {
	if err := s.validateControl(ctx); err != nil {
		return nil, err
	}
	at := time.Now().UTC()
	cancel, active := s.activeCancel()
	if active {
		cancel()
		s.logger.InfoContext(ctx, "active prefetch run cancel requested", "at", at)
		return &ecosystem.PrefetchControlReport{Action: prefetchControlCancel, ActiveRun: true, At: at}, nil
	}
	if _, err := s.metadata.RequestPrefetchControl(ctx, meta.PrefetchControlRecord{
		Action:      prefetchControlCancel,
		Reason:      "admin requested cancel",
		RequestedAt: at,
	}); err != nil {
		return nil, cacheWrap(err, "request prefetch cancel")
	}
	s.logger.InfoContext(ctx, "prefetch cancel requested", "at", at)
	return &ecosystem.PrefetchControlReport{Action: prefetchControlCancel, At: at}, nil
}

func (s *Service) RetryPrefetch(ctx context.Context) (*ecosystem.PrefetchControlReport, error) {
	if err := s.validateControl(ctx); err != nil {
		return nil, err
	}
	at := time.Now().UTC()
	if _, err := s.metadata.RequestPrefetchControl(ctx, meta.PrefetchControlRecord{
		Action:      prefetchControlRetry,
		Reason:      "admin requested retry",
		RequestedAt: at,
	}); err != nil {
		return nil, cacheWrap(err, "request prefetch retry")
	}
	s.logger.InfoContext(ctx, "prefetch retry requested", "at", at)
	return &ecosystem.PrefetchControlReport{Action: prefetchControlRetry, At: at}, nil
}

func (s *Service) validateControl(ctx context.Context) error {
	if ctx == nil {
		return cacheError("prefetch control context is required")
	}
	if err := ctx.Err(); err != nil {
		return cacheWrap(err, "prefetch control context")
	}
	if s == nil || s.metadata == nil {
		return cacheError("prefetch control service is not configured")
	}
	return nil
}

func (s *Service) setActiveCancel(runID int64, cancel context.CancelFunc) {
	if s == nil {
		return
	}
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	s.activeRunID = runID
	s.activeRunCancel = cancel
	s.logger.Debug("active prefetch cancel registered", "run_id", runID)
}

func (s *Service) clearActiveCancel(runID int64) {
	if s == nil {
		return
	}
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.activeRunID == runID {
		s.activeRunID = 0
		s.activeRunCancel = nil
		s.logger.Debug("active prefetch cancel cleared", "run_id", runID)
	}
}

func (s *Service) activeCancel() (context.CancelFunc, bool) {
	if s == nil {
		return nil, false
	}
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.activeRunCancel == nil {
		return nil, false
	}
	return s.activeRunCancel, true
}
