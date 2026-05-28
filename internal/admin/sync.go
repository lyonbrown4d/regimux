package admin

import (
	"context"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	registryref "github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/samber/oops"
)

type ManualSyncer interface {
	SubmitSync(context.Context, prefetch.SyncOptions) (prefetch.SyncJob, error)
	SyncJob(id string) (prefetch.SyncJob, bool)
}

func (s *Service) syncPage(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.sync", "sync")
	if err != nil {
		return err
	}
	data.Sync.Form = defaultSyncForm()
	data.Sync.Upstreams = s.syncUpstreamOptions(data.Sync.Form.UpstreamAlias)
	return s.render(c, "sync", "layout", data)
}

func (s *Service) syncSubmit(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.sync", "sync")
	if err != nil {
		return err
	}

	opts, form, err := s.syncOptionsFromForm(c)
	data.Sync.Form = form
	data.Sync.Upstreams = s.syncUpstreamOptions(form.UpstreamAlias)
	if err != nil {
		data.Sync.Error = err.Error()
		c.Status(fiber.StatusBadRequest)
		return s.renderSyncResponse(c, data)
	}
	if s.syncer == nil {
		data.Sync.Error = s.translate(data.Locale, "error.sync_unavailable")
		c.Status(fiber.StatusServiceUnavailable)
		return s.renderSyncResponse(c, data)
	}

	job, err := s.syncer.SubmitSync(c.Context(), opts)
	if err != nil {
		data.Sync.Error = s.syncErrorMessage(data.Locale, err)
		c.Status(fiber.StatusBadGateway)
		return s.renderSyncResponse(c, data)
	}

	data.Sync.Job = syncJobViewFromJob(job)
	data.Sync.HasJob = true
	return s.renderSyncResponse(c, data)
}

func (s *Service) syncJobPartial(c fiber.Ctx) error {
	data, err := s.pageData(c, "page.sync", "sync")
	if err != nil {
		return err
	}
	if s.syncer == nil {
		data.Sync.Error = s.translate(data.Locale, "error.sync_unavailable")
		c.Status(fiber.StatusServiceUnavailable)
		return s.render(c, "partials/sync_result", "", data)
	}
	id := strings.TrimSpace(c.Params("id"))
	job, ok := s.syncer.SyncJob(id)
	if !ok {
		data.Sync.Error = s.translate(data.Locale, "error.sync_job_not_found")
		c.Status(fiber.StatusNotFound)
		return s.render(c, "partials/sync_result", "", data)
	}
	data.Sync.Job = syncJobViewFromJob(job)
	data.Sync.HasJob = true
	return s.render(c, "partials/sync_result", "", data)
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

func (s *Service) syncUpstreamOptions(selected string) []SyncUpstreamOption {
	if selected == "" {
		selected = "hub"
	}
	options := collectionlist.NewListWithCapacity[SyncUpstreamOption](len(s.cfg.Upstreams))
	s.cfg.OrderedUpstreams().Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		options.Add(SyncUpstreamOption{
			Alias:    alias,
			Registry: upstreamCfg.Registry,
			Selected: alias == selected,
		})
		return true
	})
	return options.Values()
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

func syncJobViewFromJob(job prefetch.SyncJob) SyncJobView {
	view := SyncJobView{
		ID:         job.ID,
		Status:     job.Status,
		Target:     job.Options.Alias + "/" + job.Options.Repo + ":" + job.Options.Reference,
		Error:      job.Error,
		CreatedAt:  formatTime(job.CreatedAt),
		StartedAt:  formatTime(job.StartedAt),
		FinishedAt: formatTime(job.FinishedAt),
		Poll:       job.Status == prefetch.SyncJobStatusQueued || job.Status == prefetch.SyncJobStatusRunning,
	}
	if job.Result != nil {
		view.Result = syncResultFromReport(job.Result)
		view.HasResult = true
	}
	return view
}

func (s *Service) renderSyncResponse(c fiber.Ctx, data PageData) error {
	if strings.EqualFold(c.Get("HX-Request"), "true") {
		return s.render(c, "partials/sync_result", "", data)
	}
	return s.render(c, "sync", "layout", data)
}

func (s *Service) syncErrorMessage(locale string, err error) string {
	if err == nil {
		return ""
	}
	return s.translate(locale, "error.sync_failed") + ": " + err.Error()
}
