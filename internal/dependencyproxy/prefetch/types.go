// Package prefetch parses dependency manifests and submits explicit warm jobs.
package prefetch

import (
	"context"
	"log/slog"

	"github.com/lyonbrown4d/regimux/internal/manualsync"
)

const (
	FormatAuto          = ""
	FormatContainerRefs = "container-refs"
	FormatOCIManifest   = "oci-manifest"
	FormatGoSum         = "go.sum"
	FormatPackageLock   = "package-lock.json"
	FormatRequirements  = "requirements.txt"
	FormatPOM           = "pom.xml"
	FormatGradleWrapper = "gradle-wrapper.properties"
)

// Syncer is the scheduler/manual-sync boundary used to trigger artifact warms.
type Syncer interface {
	SubmitSync(context.Context, manualsync.SyncOptions) (manualsync.SyncJob, error)
}

type ServiceDependencies struct {
	Syncer Syncer
	Logger *slog.Logger
}

type Service struct {
	syncer Syncer
	logger *slog.Logger
}

type Source struct {
	Name   string
	Format string
	Body   []byte
}

type ParseOptions struct {
	DefaultAliases map[string]string
	Accept         string
}

type Artifact struct {
	Ecosystem string
	Alias     string
	Artifact  string
	Reference string
	Accept    string
	Source    string
	Line      int
}

type WarmRequest struct {
	Sources []Source
	Options ParseOptions
}

type WarmReport struct {
	Parsed    int
	Submitted int
	Failed    int
	Artifacts []Artifact
	Jobs      []manualsync.SyncJob
	Failures  []WarmFailure
}

type WarmFailure struct {
	Artifact Artifact
	Error    string
}
