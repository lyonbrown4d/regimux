package prefetch

import (
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func parseGoSum(source Source, opts ParseOptions) ([]Artifact, error) {
	alias, err := aliasFor(opts, ecosystem.Go)
	if err != nil {
		return nil, err
	}
	artifacts := make([]Artifact, 0)
	lineNo := 0
	for line := range strings.SplitSeq(string(source.Body), "\n") {
		lineNo++
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		module := strings.TrimSpace(fields[0])
		versionRef := strings.TrimSpace(fields[1])
		if module == "" || versionRef == "" || !strings.HasPrefix(fields[2], "h1:") {
			continue
		}
		reference, ok := goSumReference(versionRef)
		if !ok {
			continue
		}
		artifacts = append(artifacts, withDefaults(Artifact{
			Ecosystem: ecosystem.Go,
			Alias:     alias,
			Artifact:  module,
			Reference: reference,
			Line:      lineNo,
		}, source, opts))
	}
	return dedupeArtifacts(artifacts), nil
}

func goSumReference(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if version, ok := strings.CutSuffix(value, "/go.mod"); ok {
		if version == "" {
			return "", false
		}
		return "@v/" + version + ".mod", true
	}
	if strings.Contains(value, "/") {
		return "", false
	}
	return "@v/" + value + ".zip", true
}
