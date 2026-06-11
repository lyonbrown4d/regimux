package prefetch

import (
	"context"
	"log/slog"

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
	report := &WarmReport{}
	for i := range req.Sources {
		artifacts, err := Parse(req.Sources[i], req.Options)
		if err != nil {
			return report, oops.Wrapf(err, "parse dependency manifest %q", req.Sources[i].Name)
		}
		report.Artifacts = append(report.Artifacts, artifacts...)
	}
	report.Artifacts = dedupeArtifacts(report.Artifacts)
	report.Parsed = len(report.Artifacts)

	var submitErr error
	for i := range report.Artifacts {
		if err := ctx.Err(); err != nil {
			return report, oops.In("dependency-prefetch").Wrapf(err, "warm dependency manifest context")
		}
		artifact := report.Artifacts[i]
		job, err := s.syncer.SubmitSync(ctx, syncOptions(artifact))
		if err != nil {
			report.Failed++
			report.Failures = append(report.Failures, WarmFailure{
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
		report.Jobs = append(report.Jobs, job)
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
	if len(req.Sources) == 0 {
		return oops.In("dependency-prefetch").Errorf("dependency manifest source is required")
	}
	return nil
}
