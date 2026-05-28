package admin

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
)

type PrefetchController interface {
	CancelPrefetch(context.Context) (*prefetch.ControlReport, error)
	RetryPrefetch(context.Context) (*prefetch.ControlReport, error)
}

func (s *Service) prefetchCancelSubmit(c *fiber.Ctx) error {
	return s.prefetchControlSubmit(c, "action.prefetch_cancel_requested", func(ctx context.Context) (*prefetch.ControlReport, error) {
		return s.prefetch.CancelPrefetch(ctx)
	})
}

func (s *Service) prefetchRetrySubmit(c *fiber.Ctx) error {
	return s.prefetchControlSubmit(c, "action.prefetch_retry_requested", func(ctx context.Context) (*prefetch.ControlReport, error) {
		return s.prefetch.RetryPrefetch(ctx)
	})
}

func (s *Service) prefetchControlSubmit(
	c *fiber.Ctx,
	messageKey string,
	action func(context.Context) (*prefetch.ControlReport, error),
) error {
	data, err := s.pageData(c, "page.scheduler", "scheduler")
	if err != nil {
		return err
	}
	if s.prefetch == nil || action == nil {
		data.Scheduler.PrefetchControlError = translate(data.Locale, "error.prefetch_control_unavailable")
		c.Status(fiber.StatusServiceUnavailable)
		return s.render(c, "scheduler", "layout", data)
	}
	if _, actionErr := action(c.UserContext()); actionErr != nil {
		data.Scheduler.PrefetchControlError = actionErr.Error()
		c.Status(fiber.StatusBadGateway)
		return s.render(c, "scheduler", "layout", data)
	}
	data, err = s.pageData(c, "page.scheduler", "scheduler")
	if err != nil {
		return err
	}
	data.Scheduler.PrefetchControlMessage = translate(data.Locale, messageKey)
	return s.render(c, "scheduler", "layout", data)
}
