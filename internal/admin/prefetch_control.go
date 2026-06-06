package admin

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

type PrefetchController = ecosystem.PrefetchController

func (s *Service) prefetchCancelSubmit(c fiber.Ctx) error {
	return s.prefetchControlSubmit(c, "action.prefetch_cancel_requested", func(ctx context.Context) (*ecosystem.PrefetchControlReport, error) {
		return s.prefetch.CancelPrefetch(ctx)
	})
}

func (s *Service) prefetchRetrySubmit(c fiber.Ctx) error {
	return s.prefetchControlSubmit(c, "action.prefetch_retry_requested", func(ctx context.Context) (*ecosystem.PrefetchControlReport, error) {
		return s.prefetch.RetryPrefetch(ctx)
	})
}

func (s *Service) prefetchControlSubmit(
	c fiber.Ctx,
	messageKey string,
	action func(context.Context) (*ecosystem.PrefetchControlReport, error),
) error {
	data, err := s.pageData(c, "page.scheduler", "scheduler")
	if err != nil {
		return err
	}
	if s.prefetch == nil || action == nil {
		data.Scheduler.PrefetchControlError = s.translate(data.Locale, "error.prefetch_control_unavailable")
		c.Status(fiber.StatusServiceUnavailable)
		return s.render(c, "scheduler", "layout", data)
	}
	if _, actionErr := action(c.Context()); actionErr != nil {
		data.Scheduler.PrefetchControlError = actionErr.Error()
		c.Status(fiber.StatusBadGateway)
		return s.render(c, "scheduler", "layout", data)
	}
	data, err = s.pageData(c, "page.scheduler", "scheduler")
	if err != nil {
		return err
	}
	data.Scheduler.PrefetchControlMessage = s.translate(data.Locale, messageKey)
	return s.render(c, "scheduler", "layout", data)
}
