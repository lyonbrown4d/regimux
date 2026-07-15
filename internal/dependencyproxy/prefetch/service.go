package prefetch

import (
	"context"
	"log/slog"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/manualsync"
	"github.com/samber/oops"
	"go.uber.org/multierr"
)

func NewService(deps ServiceDependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		syncer: deps.Syncer,
		logger: logger.With("component", "dependency-manifest-prefetch"),
	}
}

func (s *Service) Warm(ctx context.Context, req WarmRequest) (*WarmReport, error) {
	if err := s.validate(ctx, req); err != nil {
		return nil, err
	}
	report := &WarmReport{
		Artifacts: collectionlist.NewList[Artifact](),
		Jobs:      collectionlist.NewList[manualsync.SyncJob](),
		Failures:  collectionlist.NewList[WarmFailure](),
	}
	sourceValues := req.Sources.Values()
	for i := range sourceValues {
		artifacts, err := Parse(sourceValues[i], req.Options)
		if err != nil {
			return report, oops.Wrapf(err, "parse dependency manifest %q", sourceValues[i].Name)
		}
		report.Artifacts.Merge(artifacts)
	}
	report.Artifacts = dedupeArtifacts(report.Artifacts)
	report.Parsed = report.Artifacts.Len()

	var submitErr error
	artifactValues := report.Artifacts.Values()
	for i := range artifactValues {
		if err := ctx.Err(); err != nil {
			return report, oops.In("dependency-prefetch").Wrapf(err, "warm dependency manifest context")
		}
		artifact := artifactValues[i]
		job, err := s.syncer.SubmitSync(ctx, syncOptions(artifact))
		if err != nil {
			report.Failed++
			report.Failures.Add(WarmFailure{
				Artifact: artifact,
				Error:    err.Error(),
			})
			submitErr = multierr.Append(submitErr, err)
			s.logger.WarnContext(ctx,
				"manifest dependency warm job failed",
				"ecosystem", artifact.Ecosystem,
				"alias", artifact.Alias,
				"artifact", artifact.Artifact,
				"reference", artifact.Reference,
				"error", err,
			)
			continue
		}
		report.Submitted++
		report.Jobs.Add(job)
	}
	if submitErr != nil {
		return report, oops.Wrapf(submitErr, "submit manifest dependency warm jobs")
	}
	return report, nil
}

func (s *Service) validate(ctx context.Context, req WarmRequest) error {
	if ctx == nil {
		return oops.In("dependency-prefetch").Errorf("warm context is required")
	}
	if err := ctx.Err(); err != nil {
		return oops.In("dependency-prefetch").Wrapf(err, "warm context")
	}
	if s == nil || s.syncer == nil {
		return oops.In("dependency-prefetch").Errorf("manifest dependency prefetch service is not configured")
	}
	if req.Sources == nil || req.Sources.Len() == 0 {
		return oops.In("dependency-prefetch").Errorf("dependency manifest source is required")
	}
	return nil
}
