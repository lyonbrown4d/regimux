package admin

import (
	"github.com/gofiber/fiber/v3"
	"github.com/samber/oops"
)

type cleanupAccepted struct {
	Status string `json:"status"`
	Scope  string `json:"scope"`
}

type cleanupAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *Service) schedulerCleanupJSON(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if s.scheduler == nil {
		return writeCleanupJSON(c, fiber.StatusServiceUnavailable, cleanupAPIError{
			Code: "cleanup_unavailable", Message: "Cleanup control is not configured.",
		})
	}
	if err := s.scheduler.TriggerCleanup(c.Context()); err != nil {
		s.logger.WarnContext(c.Context(), "manual cache cleanup submission failed", "error", err)
		return writeCleanupJSON(c, fiber.StatusServiceUnavailable, cleanupAPIError{
			Code: "cleanup_trigger_failed", Message: "Cleanup could not be submitted.",
		})
	}
	s.logger.InfoContext(c.Context(), "manual cache cleanup submitted", "scope", "all")
	return writeCleanupJSON(c, fiber.StatusAccepted, cleanupAccepted{Status: "accepted", Scope: "all"})
}

func writeCleanupJSON(c fiber.Ctx, status int, value any) error {
	if err := c.Status(status).JSON(value); err != nil {
		return oops.In("admin").Wrapf(err, "write cleanup API response")
	}
	return nil
}
