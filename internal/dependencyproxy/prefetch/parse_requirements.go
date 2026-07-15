package prefetch

import (
	"regexp"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

var requirementsNameSeparatorRE = regexp.MustCompile(`[-_.]+`)

func parseRequirements(source Source, opts ParseOptions) (*collectionlist.List[Artifact], error) {
	alias, err := aliasFor(opts, ecosystem.PyPI)
	if err != nil {
		return nil, err
	}
	artifacts := collectionlist.NewList[Artifact]()
	lineNo := 0
	for line := range strings.SplitSeq(string(source.Body), "\n") {
		lineNo++
		name, ok := requirementProjectName(line)
		if !ok {
			continue
		}
		artifacts.Add(withDefaults(Artifact{
			Ecosystem: ecosystem.PyPI,
			Alias:     alias,
			Artifact:  "pypi/simple/" + normalizeRequirementProjectName(name),
			Reference: "index.html",
			Line:      lineNo,
		}, source, opts))
	}
	return dedupeArtifacts(artifacts), nil
}

func requirementProjectName(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
		return "", false
	}
	if egg, ok := strings.CutPrefix(line, "git+"); ok {
		return requirementEggName(egg)
	}
	if strings.Contains(line, "://") {
		if egg, ok := requirementEggName(line); ok {
			return egg, true
		}
		return "", false
	}
	if before, _, ok := strings.Cut(line, " #"); ok {
		line = strings.TrimSpace(before)
	}
	name, _, ok := strings.Cut(line, "==")
	if !ok {
		return "", false
	}
	name = strings.TrimSpace(name)
	if before, _, ok := strings.Cut(name, "["); ok {
		name = strings.TrimSpace(before)
	}
	return name, name != ""
}

func requirementEggName(line string) (string, bool) {
	_, fragment, ok := strings.Cut(line, "#")
	if !ok {
		return "", false
	}
	for part := range strings.SplitSeq(fragment, "&") {
		if name, ok := strings.CutPrefix(part, "egg="); ok {
			name = strings.TrimSpace(name)
			if name != "" {
				return name, true
			}
		}
	}
	return "", false
}

func normalizeRequirementProjectName(name string) string {
	name = strings.TrimSpace(name)
	name = requirementsNameSeparatorRE.ReplaceAllString(name, "-")
	return strings.ToLower(name)
}
