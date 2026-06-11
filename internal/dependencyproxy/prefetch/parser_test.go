package prefetch

import (
	"context"
	"errors"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/manualsync"
)

func TestParseGoSumProducesGoModuleZipAndModArtifacts(t *testing.T) {
	artifacts, err := Parse(Source{
		Name: "go.sum",
		Body: []byte("example.com/mod v1.2.3 h1:abc\nexample.com/mod v1.2.3/go.mod h1:def\nexample.com/mod v1.2.3 h1:abc\n"),
	}, ParseOptions{DefaultAliases: map[string]string{ecosystem.Go: "gomod"}})
	requireNoError(t, err)
	assertArtifacts(t, artifacts, []Artifact{
		{Ecosystem: ecosystem.Go, Alias: "gomod", Artifact: "example.com/mod", Reference: "@v/v1.2.3.zip", Source: "go.sum", Line: 1},
		{Ecosystem: ecosystem.Go, Alias: "gomod", Artifact: "example.com/mod", Reference: "@v/v1.2.3.mod", Source: "go.sum", Line: 2},
	})
}

func TestParsePackageLockProducesNPMTarballArtifacts(t *testing.T) {
	body := []byte(`{
		"lockfileVersion": 3,
		"packages": {
			"": {},
			"node_modules/left-pad": {"resolved": "https://registry.npmjs.org/left-pad/-/left-pad-1.3.0.tgz"},
			"node_modules/@scope/pkg": {"resolved": "https://registry.npmjs.org/@scope/pkg/-/pkg-2.0.0.tgz"}
		}
	}`)
	artifacts, err := Parse(Source{Name: "package-lock.json", Body: body}, ParseOptions{
		DefaultAliases: map[string]string{ecosystem.NPM: "npmjs"},
	})
	requireNoError(t, err)
	assertArtifacts(t, artifacts, []Artifact{
		{Ecosystem: ecosystem.NPM, Alias: "npmjs", Artifact: "@scope/pkg", Reference: "tarball:pkg-2.0.0.tgz", Source: "package-lock.json"},
		{Ecosystem: ecosystem.NPM, Alias: "npmjs", Artifact: "left-pad", Reference: "tarball:left-pad-1.3.0.tgz", Source: "package-lock.json"},
	})
}

func TestParseRequirementsProducesPyPISimpleArtifacts(t *testing.T) {
	body := []byte("Django==5.0.1\nrequests[security]==2.32.0 # pinned\nhttps://example.test/pkg.tar.gz#egg=My_Package\n")
	artifacts, err := Parse(Source{Name: "requirements.txt", Body: body}, ParseOptions{
		DefaultAliases: map[string]string{ecosystem.PyPI: "pypi"},
	})
	requireNoError(t, err)
	assertArtifacts(t, artifacts, []Artifact{
		{Ecosystem: ecosystem.PyPI, Alias: "pypi", Artifact: "pypi/simple/django", Reference: "index.html", Source: "requirements.txt", Line: 1},
		{Ecosystem: ecosystem.PyPI, Alias: "pypi", Artifact: "pypi/simple/requests", Reference: "index.html", Source: "requirements.txt", Line: 2},
		{Ecosystem: ecosystem.PyPI, Alias: "pypi", Artifact: "pypi/simple/my-package", Reference: "index.html", Source: "requirements.txt", Line: 3},
	})
}

func TestParsePOMProducesMavenJarArtifacts(t *testing.T) {
	body := []byte(`<project>
		<dependencies>
			<dependency>
				<groupId>org.slf4j</groupId>
				<artifactId>slf4j-api</artifactId>
				<version>2.0.12</version>
			</dependency>
			<dependency>
				<groupId>demo</groupId>
				<artifactId>dynamic</artifactId>
				<version>${dynamic.version}</version>
			</dependency>
		</dependencies>
	</project>`)
	artifacts, err := Parse(Source{Name: "pom.xml", Body: body}, ParseOptions{
		DefaultAliases: map[string]string{ecosystem.Maven: "central"},
	})
	requireNoError(t, err)
	assertArtifacts(t, artifacts, []Artifact{
		{Ecosystem: ecosystem.Maven, Alias: "central", Artifact: "org/slf4j/slf4j-api/2.0.12", Reference: "slf4j-api-2.0.12.jar", Source: "pom.xml"},
	})
}

func TestParseContainerRefsUsesDefaultAliasAndExplicitAlias(t *testing.T) {
	artifacts, err := Parse(Source{
		Name:   "images.txt",
		Format: FormatContainerRefs,
		Body:   []byte("library/nginx:1.25\ncontainer:mirror/library/redis@sha256:abc\n"),
	}, ParseOptions{DefaultAliases: map[string]string{ecosystem.Container: "hub"}})
	requireNoError(t, err)
	assertArtifacts(t, artifacts, []Artifact{
		{Ecosystem: ecosystem.Container, Alias: "hub", Artifact: "library/nginx", Reference: "1.25", Source: "images.txt", Line: 1},
		{Ecosystem: ecosystem.Container, Alias: "mirror", Artifact: "library/redis", Reference: "sha256:abc", Source: "images.txt", Line: 2},
	})
}

func TestServiceWarmSubmitsDedupeSyncJobs(t *testing.T) {
	syncer := &recordingSyncer{}
	service := NewService(ServiceDependencies{Syncer: syncer})
	report, err := service.Warm(context.Background(), WarmRequest{
		Sources: []Source{{
			Name: "go.sum",
			Body: []byte("example.com/mod v1.2.3 h1:abc\nexample.com/mod v1.2.3 h1:abc\n"),
		}},
		Options: ParseOptions{DefaultAliases: map[string]string{ecosystem.Go: "gomod"}},
	})
	requireNoError(t, err)
	if report.Parsed != 1 || report.Submitted != 1 || report.Failed != 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(syncer.submitted) != 1 || syncer.submitted[0].Reference != "@v/v1.2.3.zip" {
		t.Fatalf("unexpected submissions: %#v", syncer.submitted)
	}
}

func TestServiceWarmReportsSubmitFailures(t *testing.T) {
	syncer := &recordingSyncer{err: errors.New("queue full")}
	service := NewService(ServiceDependencies{Syncer: syncer})
	report, err := service.Warm(context.Background(), WarmRequest{
		Sources: []Source{{
			Name: "requirements.txt",
			Body: []byte("Django==5.0.1\n"),
		}},
		Options: ParseOptions{DefaultAliases: map[string]string{ecosystem.PyPI: "pypi"}},
	})
	if err == nil {
		t.Fatal("expected warm error")
	}
	if report == nil || report.Parsed != 1 || report.Submitted != 0 || report.Failed != 1 || len(report.Failures) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

type recordingSyncer struct {
	err       error
	submitted []manualsync.SyncOptions
}

func (s *recordingSyncer) SubmitSync(_ context.Context, opts manualsync.SyncOptions) (manualsync.SyncJob, error) {
	if s.err != nil {
		return manualsync.SyncJob{}, s.err
	}
	s.submitted = append(s.submitted, opts)
	return manualsync.SyncJob{ID: "job"}, nil
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func assertArtifacts(t *testing.T, got, want []Artifact) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("artifact count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("artifact[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
