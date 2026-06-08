package admin

import (
	"log/slog"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/lyonbrown4d/regimux/internal/api"
	authpkg "github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/oops"
)

const basePath = "/admin"

type Service struct {
	cfg       config.Config
	metadata  meta.Store
	runtimes  *collectionlist.List[ecosystem.Runtime]
	version   build.Version
	logger    *slog.Logger
	auth      *authpkg.Service
	messages  *Messages
	mapper    *AdminMapper
	syncer    ManualSyncer
	prefetch  PrefetchController
	scheduler SchedulerController
	startedAt time.Time
}

var _ api.FiberRoute = (*Service)(nil)

func NewService(deps Dependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	mapper := deps.Mapper
	if mapper == nil {
		mapper = NewAdminMapper()
	}
	service := &Service{
		cfg:       deps.Config,
		metadata:  deps.Metadata,
		runtimes:  deps.Runtimes,
		version:   deps.Version,
		logger:    logger.With("component", "admin"),
		auth:      deps.Auth,
		messages:  deps.Messages,
		mapper:    mapper,
		syncer:    deps.Syncer,
		prefetch:  deps.Prefetch,
		scheduler: deps.Scheduler,
		startedAt: time.Now(),
	}
	service.logger.Info("admin service configured", "base_path", basePath, "auth_enabled", service.adminAuthEnabled())
	return service
}

func (s *Service) RegisterFiber(app *fiber.App) {
	if s == nil || app == nil {
		return
	}
	s.logger.Info("registering admin routes", "base_path", basePath)
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
	group.Post("/prefetch/cancel", s.prefetchCancelSubmit)
	group.Post("/prefetch/retry", s.prefetchRetrySubmit)
	group.Post("/scheduler/cleanup", s.schedulerCleanupSubmit)
	group.Post("/scheduler/probe", s.schedulerProbeSubmit)
	group.Get("/sync", s.syncPage)
	group.Post("/sync", s.syncSubmit)
	group.Get("/sync/jobs/:id", s.syncJobPartial)
	group.Get("/audit", s.auditPage)
	group.Get("/config", s.configPage)
	group.Get("/partials/upstream-health", s.upstreamHealthPartial)
	s.logger.Info("admin routes registered", "base_path", basePath)
}

func (s *Service) dashboard(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.dashboard", "dashboard")
	if err != nil {
		return err
	}
	data.RecentPulls = data.Pulls
	return s.render(c, "dashboard", "layout", data)
}

func (s *Service) upstreamsPage(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.upstreams", "upstreams")
	if err != nil {
		return err
	}
	return s.render(c, "upstreams", "layout", data)
}

func (s *Service) pullsPage(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.pulls", "pulls")
	if err != nil {
		return err
	}
	return s.render(c, "pulls", "layout", data)
}

func (s *Service) activityPage(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.activity", "activity")
	if err != nil {
		return err
	}
	return s.render(c, "activity", "layout", data)
}

func (s *Service) cachePage(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.cache", "cache")
	if err != nil {
		return err
	}
	return s.render(c, "cache", "layout", data)
}

func (s *Service) storagePage(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.storage", "storage")
	if err != nil {
		return err
	}
	return s.render(c, "storage", "layout", data)
}

func (s *Service) schedulerPage(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.scheduler", "scheduler")
	if err != nil {
		return err
	}
	return s.render(c, "scheduler", "layout", data)
}

func (s *Service) auditPage(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.audit", "audit")
	if err != nil {
		return err
	}
	return s.render(c, "audit", "layout", data)
}

func (s *Service) configPage(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.config", "config")
	if err != nil {
		return err
	}
	return s.render(c, "config", "layout", data)
}

func (s *Service) upstreamHealthPartial(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.upstream_health", "upstreams")
	if err != nil {
		return err
	}
	return s.render(c, "partials/upstream_health", "", data)
}

func (s *Service) pageData(c fiber.Ctx, titleKey, active string) (PageData, error) {
	now := time.Now()
	locale := localeFromRequest(c)
	rows, err := s.metadataRows(c.Context(), now, active)
	if err != nil {
		return PageData{}, err
	}
	upstreams, err := s.upstreamRows(now, rows.upstreams)
	if err != nil {
		return PageData{}, err
	}
	cache, err := s.cacheSummary(rows)
	if err != nil {
		return PageData{}, err
	}
	pulls, err := s.mapper.PullRows(rows.pulls)
	if err != nil {
		return PageData{}, err
	}
	summary := s.summary(rows, upstreams, now)
	scheduler, err := s.schedulerSummary(c.Context())
	if err != nil {
		return PageData{}, err
	}
	activity, err := s.activitySummary(rows)
	if err != nil {
		return PageData{}, err
	}
	storage, err := s.storageSummary(rows)
	if err != nil {
		return PageData{}, err
	}

	return PageData{
		Title:              s.translate(locale, titleKey),
		Active:             active,
		ActiveLabel:        s.translate(locale, "nav."+active),
		GeneratedAt:        formatTime(now),
		BasePath:           basePath,
		Locale:             locale,
		HTMLLang:           htmlLang(locale),
		LanguageSwitchHref: languageSwitchHref(c, oppositeLocale(locale)),
		CSRFToken:          csrf.TokenFromContext(c),
		Summary:            summary,
		Upstreams:          upstreams,
		Pulls:              pulls,
		Cache:              cache,
		Activity:           activity,
		Storage:            storage,
		Audit:              auditSummary(s.cfg),
		Scheduler:          scheduler,
		ConfigRows:         configRows(s.cfg, s.configuredUpstreams()),
		ConfigSources:      configSourceRows(locale, s.messages),
	}, nil
}

func (s *Service) translate(locale, key string) string {
	if s == nil || s.messages == nil {
		return key
	}
	return s.messages.Translate(locale, key)
}

func (s *Service) render(c fiber.Ctx, name, templateName string, data PageData) error {
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

func languageSwitchHref(c fiber.Ctx, locale string) string {
	if c == nil {
		return basePath + "/?lang=" + locale
	}
	return c.Path() + "?lang=" + locale
}
