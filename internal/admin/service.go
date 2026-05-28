package admin

import (
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/lyonbrown4d/regimux/internal/api"
	authpkg "github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/samber/oops"
)

const basePath = "/admin"

type Service struct {
	cfg       config.Config
	metadata  meta.Store
	upstream  *upstream.Client
	version   build.Version
	logger    *slog.Logger
	auth      *authpkg.Service
	syncer    ManualSyncer
	startedAt time.Time
}

var _ api.FiberRoute = (*Service)(nil)

func NewService(deps Dependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		cfg:       deps.Config,
		metadata:  deps.Metadata,
		upstream:  deps.Upstream,
		version:   deps.Version,
		logger:    logger.With("component", "admin"),
		auth:      deps.Auth,
		syncer:    deps.Syncer,
		startedAt: time.Now(),
	}
}

func (s *Service) RegisterFiber(app *fiber.App) {
	if s == nil || app == nil {
		return
	}
	app.Use(basePath, s.requireAdminAuth)
	app.Get(basePath, s.dashboard)
	group := app.Group(basePath)
	group.Get("/", s.dashboard)
	group.Get("/upstreams", s.upstreamsPage)
	group.Get("/pulls", s.pullsPage)
	group.Get("/activity", s.activityPage)
	group.Get("/cache", s.cachePage)
	group.Get("/storage", s.storagePage)
	group.Get("/scheduler", s.schedulerPage)
	group.Get("/sync", s.syncPage)
	group.Post("/sync", s.syncSubmit)
	group.Get("/audit", s.auditPage)
	group.Get("/config", s.configPage)
	group.Get("/partials/upstream-health", s.upstreamHealthPartial)
}

func (s *Service) requireAdminAuth(c *fiber.Ctx) error {
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
	if _, err := s.auth.AuthenticateBasic(c.UserContext(), username, password); err != nil {
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

func writeAdminUnauthorized(c *fiber.Ctx) error {
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

func (s *Service) dashboard(c *fiber.Ctx) error {
	data, err := s.pageData(c, "page.dashboard", "dashboard")
	if err != nil {
		return err
	}
	data.RecentPulls = limitPulls(data.Pulls, 10)
	return s.render(c, "dashboard", "layout", data)
}

func (s *Service) upstreamsPage(c *fiber.Ctx) error {
	data, err := s.pageData(c, "page.upstreams", "upstreams")
	if err != nil {
		return err
	}
	return s.render(c, "upstreams", "layout", data)
}

func (s *Service) pullsPage(c *fiber.Ctx) error {
	data, err := s.pageData(c, "page.pulls", "pulls")
	if err != nil {
		return err
	}
	return s.render(c, "pulls", "layout", data)
}

func (s *Service) activityPage(c *fiber.Ctx) error {
	data, err := s.pageData(c, "page.activity", "activity")
	if err != nil {
		return err
	}
	return s.render(c, "activity", "layout", data)
}

func (s *Service) cachePage(c *fiber.Ctx) error {
	data, err := s.pageData(c, "page.cache", "cache")
	if err != nil {
		return err
	}
	return s.render(c, "cache", "layout", data)
}

func (s *Service) storagePage(c *fiber.Ctx) error {
	data, err := s.pageData(c, "page.storage", "storage")
	if err != nil {
		return err
	}
	return s.render(c, "storage", "layout", data)
}

func (s *Service) schedulerPage(c *fiber.Ctx) error {
	data, err := s.pageData(c, "page.scheduler", "scheduler")
	if err != nil {
		return err
	}
	return s.render(c, "scheduler", "layout", data)
}

func (s *Service) auditPage(c *fiber.Ctx) error {
	data, err := s.pageData(c, "page.audit", "audit")
	if err != nil {
		return err
	}
	return s.render(c, "audit", "layout", data)
}

func (s *Service) configPage(c *fiber.Ctx) error {
	data, err := s.pageData(c, "page.config", "config")
	if err != nil {
		return err
	}
	return s.render(c, "config", "layout", data)
}

func (s *Service) upstreamHealthPartial(c *fiber.Ctx) error {
	data, err := s.pageData(c, "page.upstream_health", "upstreams")
	if err != nil {
		return err
	}
	return s.render(c, "partials/upstream_health", "", data)
}

func (s *Service) pageData(c *fiber.Ctx, titleKey, active string) (PageData, error) {
	now := time.Now()
	locale := localeFromRequest(c)
	rows, err := s.metadataRows(c.UserContext(), now)
	if err != nil {
		return PageData{}, err
	}
	upstreams := s.upstreamRows(now)
	cache := cacheSummary(rows, now)
	pulls := pullRows(rows.pulls)
	summary := s.summary(rows, upstreams, pulls, now)

	return PageData{
		Title:              translate(locale, titleKey),
		Active:             active,
		ActiveLabel:        translate(locale, "nav."+active),
		GeneratedAt:        formatTime(now),
		BasePath:           basePath,
		Locale:             locale,
		HTMLLang:           htmlLang(locale),
		LanguageSwitchHref: languageSwitchHref(c, oppositeLocale(locale)),
		Summary:            summary,
		Upstreams:          upstreams,
		Pulls:              pulls,
		Cache:              cache,
		Activity:           activitySummary(rows),
		Storage:            storageSummary(rows),
		Audit:              auditSummary(s.cfg),
		Scheduler:          s.schedulerSummary(),
		ConfigRows:         configRows(s.cfg),
		ConfigSources:      configSourceRows(locale),
	}, nil
}

func (s *Service) render(c *fiber.Ctx, name, templateName string, data PageData) error {
	c.Type("html", "utf-8")
	var err error
	if templateName == "" {
		err = c.Render(name, data)
	} else {
		err = c.Render(name, data, templateName)
	}
	if err != nil {
		return oops.In("admin").With("template", name).Wrapf(err, "render admin template")
	}
	return nil
}

func languageSwitchHref(c *fiber.Ctx, locale string) string {
	if c == nil {
		return basePath + "/?lang=" + locale
	}
	return c.Path() + "?lang=" + locale
}
