package prefetch

import (
	"net/url"
	"path"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func parseGradleWrapper(source Source, opts ParseOptions) ([]Artifact, error) {
	alias, err := aliasFor(opts, ecosystem.Dist)
	if err != nil {
		return nil, err
	}
	artifacts := make([]Artifact, 0, 1)
	lineNo := 0
	for line := range strings.SplitSeq(string(source.Body), "\n") {
		lineNo++
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(key) != "distributionUrl" {
			continue
		}
		if artifact, ok := gradleDistributionArtifact(source, opts, alias, value, lineNo); ok {
			artifacts = append(artifacts, artifact)
		}
	}
	return dedupeArtifacts(artifacts), nil
}

func gradleDistributionArtifact(source Source, opts ParseOptions, alias, rawURL string, line int) (Artifact, bool) {
	distributionURL := unescapeGradlePropertyValue(rawURL)
	parsed, err := url.Parse(strings.TrimSpace(distributionURL))
	if err != nil || parsed.Path == "" {
		return Artifact{}, false
	}
	file := path.Base(parsed.Path)
	if file == "." || file == "/" || file == "" {
		return Artifact{}, false
	}
	return withDefaults(Artifact{
		Ecosystem: ecosystem.Dist,
		Alias:     alias,
		Artifact:  "dist",
		Reference: file,
		Line:      line,
	}, source, opts), true
}

func unescapeGradlePropertyValue(value string) string {
	value = strings.TrimSpace(value)
	replacer := strings.NewReplacer(`\:`, `:`, `\/`, `/`, `\\`, `\`)
	return replacer.Replace(value)
}
