package admin

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/samber/oops"
)

func (s *Service) requireAdminAuth(c fiber.Ctx) error {
	if !s.adminAuthEnabled() {
		if err := c.Next(); err != nil {
			return oops.In("admin").Wrapf(err, "continue admin request")
		}
		return nil
	}
	if s.auth == nil {
		return writeAdminUnauthorized(c)
	}
	username, password, ok := basicAuthFromHeader(c.Get(fiber.HeaderAuthorization))
	if !ok {
		return writeAdminUnauthorized(c)
	}
	if _, err := s.auth.AuthenticateBasic(c.Context(), username, password); err != nil {
		return writeAdminUnauthorized(c)
	}
	if err := c.Next(); err != nil {
		return oops.In("admin").Wrapf(err, "continue authenticated admin request")
	}
	return nil
}

func (s *Service) adminAuthEnabled() bool {
	if s == nil {
		return false
	}
	if s.cfg.Auth.Enabled {
		return true
	}
	return s.auth != nil && s.auth.Enabled()
}

func writeAdminUnauthorized(c fiber.Ctx) error {
	c.Set(fiber.HeaderWWWAuthenticate, `Basic realm="regimux admin"`)
	if err := c.SendStatus(http.StatusUnauthorized); err != nil {
		return oops.In("admin").Wrapf(err, "write admin unauthorized")
	}
	return nil
}

func basicAuthFromHeader(header string) (string, string, bool) {
	scheme, payload, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Basic") {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
	if err != nil {
		return "", "", false
	}
	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok || username == "" {
		return "", "", false
	}
	return username, password, true
}
