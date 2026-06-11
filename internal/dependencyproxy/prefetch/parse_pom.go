package prefetch

import (
	"encoding/xml"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/oops"
)

type pomProject struct {
	Dependencies         []pomDependency `xml:"dependencies>dependency"`
	DependencyManagement []pomDependency `xml:"dependencyManagement>dependencies>dependency"`
}

type pomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
	Optional   string `xml:"optional"`
}

func parsePOM(source Source, opts ParseOptions) ([]Artifact, error) {
	alias, err := aliasFor(opts, ecosystem.Maven)
	if err != nil {
		return nil, err
	}
	var project pomProject
	if err := xml.Unmarshal(source.Body, &project); err != nil {
		return nil, oops.In("dependency-prefetch").Wrapf(err, "decode pom.xml")
	}
	dependencies := append(project.Dependencies, project.DependencyManagement...)
	artifacts := make([]Artifact, 0, len(dependencies))
	for i := range dependencies {
		artifact, ok := mavenArtifactFromDependency(source, opts, alias, dependencies[i])
		if ok {
			artifacts = append(artifacts, artifact)
		}
	}
	return dedupeArtifacts(artifacts), nil
}

func mavenArtifactFromDependency(source Source, opts ParseOptions, alias string, dep pomDependency) (Artifact, bool) {
	groupID := strings.TrimSpace(dep.GroupID)
	artifactID := strings.TrimSpace(dep.ArtifactID)
	version := strings.TrimSpace(dep.Version)
	if groupID == "" || artifactID == "" || version == "" || strings.Contains(version, "${") {
		return Artifact{}, false
	}
	repository := strings.ReplaceAll(groupID, ".", "/") + "/" + artifactID + "/" + version
	return withDefaults(Artifact{
		Ecosystem: ecosystem.Maven,
		Alias:     alias,
		Artifact:  repository,
		Reference: artifactID + "-" + version + ".jar",
	}, source, opts), true
}
