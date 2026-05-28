package admin

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	registryref "github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/samber/oops"
)

const manualSyncTimeout = 5 * time.Minute

type ManualSyncer interface {
	Sync(context.Context, prefetch.SyncOptions) (*prefetch.SyncReport, error)
}

func (s *Service) syncPage(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.sync", "sync")
	if err != nil {
		return err
	}
	data.Sync.Form = defaultSyncForm()
	return s.render(c, "sync", "layout", data)
}

func (s *Service) syncSubmit(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.sync", "sync")
	if err != nil {
		return err
	}

	opts, form, err := s.syncOptionsFromForm(c)
	data.Sync.Form = form
	if err != nil {
		data.Sync.Error = err.Error()
		c.Status(fiber.StatusBadRequest)
		return s.render(c, "sync", "layout", data)
	}
	if s.syncer == nil {
		data.Sync.Error = s.translate(data.Locale, "error.sync_unavailable")
		c.Status(fiber.StatusServiceUnavailable)
		return s.render(c, "sync", "layout", data)
	}

	ctx, cancel := context.WithTimeout(c.Context(), manualSyncTimeout)
	defer cancel()
	report, err := s.syncer.Sync(ctx, opts)
	if err != nil {
		data.Sync.Error = s.syncErrorMessage(data.Locale, err)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			c.Status(fiber.StatusGatewayTimeout)
		} else {
			c.Status(fiber.StatusBadGateway)
		}
		return s.render(c, "sync", "layout", data)
	}

	data.Sync.Result = syncResultFromReport(report)
	data.Sync.HasResult = true
	return s.render(c, "sync", "layout", data)
}

func (s *Service) syncOptionsFromForm(c fiber.Ctx) (prefetch.SyncOptions, SyncForm, error) {
	form := SyncForm{
		UpstreamAlias: strings.TrimSpace(c.FormValue("upstream_alias")),
		Repository:    strings.TrimSpace(c.FormValue("repository")),
		Reference:     strings.TrimSpace(c.FormValue("reference")),
	}
	if form.UpstreamAlias == "" {
		form.UpstreamAlias = "hub"
	}
	if form.Repository == "" {
		return prefetch.SyncOptions{}, form, oops.In("admin").Errorf("repository is required")
	}

	repo, form, err := syncRepositoryAndReference(form)
	if err != nil {
		return prefetch.SyncOptions{}, form, err
	}
	route, form, err := s.syncRoute(form, repo)
	if err != nil {
		return prefetch.SyncOptions{}, form, err
	}

	return prefetch.SyncOptions{
		Alias:     route.Alias,
		Repo:      route.Repo,
		Reference: route.Reference,
		Accept:    s.cfg.Scheduler.Prefetch.Accept,
	}, form, nil
}

func syncRepositoryAndReference(form SyncForm) (string, SyncForm, error) {
	repo, embeddedReference, err := splitRepositoryReference(form.Repository)
	if err != nil {
		return "", form, err
	}
	if embeddedReference != "" {
		if form.Reference != "" && form.Reference != embeddedReference {
			return "", form, oops.In("admin").Errorf("repository and reference fields disagree")
		}
		form.Reference = embeddedReference
	}
	if form.Reference == "" {
		form.Reference = "latest"
	}
	return repo, form, nil
}

func (s *Service) syncRoute(form SyncForm, repo string) (*registryref.Route, SyncForm, error) {
	route, err := registryref.ParseManifestPath("/v2/" + form.UpstreamAlias + "/" + repo + "/manifests/" + form.Reference)
	if err != nil {
		return nil, form, oops.In("admin").Wrapf(err, "invalid sync target")
	}
	upstreamCfg, ok := s.cfg.Upstreams[route.Alias]
	if !ok {
		return nil, form, oops.In("admin").With("alias", route.Alias).Errorf("unknown upstream alias %q", route.Alias)
	}
	*route = route.WithDefaultNamespace(upstreamCfg.DefaultNamespace)
	form.Repository = route.Repo
	form.Reference = route.Reference
	return route, form, nil
}

func defaultSyncForm() SyncForm {
	return SyncForm{
		UpstreamAlias: "hub",
		Reference:     "latest",
	}
}

func splitRepositoryReference(value string) (string, string, error) {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return "", "", oops.In("admin").Errorf("repository is required")
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return "", "", oops.In("admin").Errorf("repository cannot contain whitespace")
	}
	if repo, reference, ok := strings.Cut(value, "@"); ok {
		if repo == "" || reference == "" {
			return "", "", oops.In("admin").Errorf("repository digest reference is invalid")
		}
		return repo, reference, nil
	}
	colon := strings.LastIndex(value, ":")
	slash := strings.LastIndex(value, "/")
	if colon > slash {
		repo := value[:colon]
		reference := value[colon+1:]
		if repo == "" || reference == "" {
			return "", "", oops.In("admin").Errorf("repository tag reference is invalid")
		}
		return repo, reference, nil
	}
	return value, "", nil
}

func syncResultFromReport(report *prefetch.SyncReport) SyncResult {
	if report == nil {
		return SyncResult{}
	}
	return SyncResult{
		Alias:              report.Alias,
		Repository:         report.Repo,
		Reference:          report.Reference,
		ManifestDigest:     report.ManifestDigest,
		MediaType:          report.MediaType,
		LayerCount:         report.LayerCount,
		BlobCount:          report.BlobCount,
		ChildManifestCount: report.ChildManifestCount,
		Duration:           formatDuration(report.Duration),
	}
}

func (s *Service) syncErrorMessage(locale string, err error) string {
	if err == nil {
		return ""
	}
	return s.translate(locale, "error.sync_failed") + ": " + err.Error()
}
