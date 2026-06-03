package admin

import (
	"context"
	"strings"

	"github.com/gofiber/fiber/v3"
)

type SchedulerController interface {
	TriggerCleanup(context.Context) error
	TriggerProbe(context.Context, string, string) error
}

func (s *Service) schedulerCleanupSubmit(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.scheduler", "scheduler")
	if err != nil {
		return err
	}

	if s.scheduler == nil {
		data.Scheduler.CleanupControlError = s.translate(data.Locale, "error.cleanup_control_unavailable")
		c.Status(fiber.StatusServiceUnavailable)
		return s.render(c, "scheduler", "layout", data)
	}

	if err := s.scheduler.TriggerCleanup(c.Context()); err != nil {
		data.Scheduler.CleanupControlError = err.Error()
		c.Status(fiber.StatusBadGateway)
		return s.render(c, "scheduler", "layout", data)
	}

	data, err = s.pageData(c, "page.scheduler", "scheduler")
	if err != nil {
		return err
	}
	data.Scheduler.CleanupControlMessage = s.translate(data.Locale, "action.cleanup_trigger_requested")
	return s.render(c, "scheduler", "layout", data)
}

func (s *Service) schedulerProbeSubmit(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.scheduler", "scheduler")
	if err != nil {
		return err
	}
	ecosystemName := strings.TrimSpace(c.FormValue("ecosystem"))
	alias := strings.TrimSpace(c.FormValue("alias"))
	if ecosystemName == "" {
		data.Scheduler.ProbeControlError = s.translate(data.Locale, "error.probe_ecosystem_required")
		c.Status(fiber.StatusBadRequest)
		return s.render(c, "scheduler", "layout", data)
	}
	if alias == "" {
		data.Scheduler.ProbeControlError = s.translate(data.Locale, "error.probe_alias_required")
		c.Status(fiber.StatusBadRequest)
		return s.render(c, "scheduler", "layout", data)
	}
	if s.scheduler == nil {
		data.Scheduler.ProbeControlError = s.translate(data.Locale, "error.probe_control_unavailable")
		c.Status(fiber.StatusServiceUnavailable)
		return s.render(c, "scheduler", "layout", data)
	}

	if err := s.scheduler.TriggerProbe(c.Context(), ecosystemName, alias); err != nil {
		data.Scheduler.ProbeControlError = err.Error()
		c.Status(fiber.StatusBadGateway)
		return s.render(c, "scheduler", "layout", data)
	}

	data, err = s.pageData(c, "page.scheduler", "scheduler")
	if err != nil {
		return err
	}
	data.Scheduler.ProbeControlMessage = s.translate(data.Locale, "action.probe_trigger_requested")
	return s.render(c, "scheduler", "layout", data)
}
