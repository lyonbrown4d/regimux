package prefetch

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/oops"
)

type packageLock struct {
	Packages     map[string]packageLockPackage    `json:"packages"`
	Dependencies map[string]packageLockDependency `json:"dependencies"`
}

type packageLockPackage struct {
	Resolved string `json:"resolved"`
}

type packageLockDependency struct {
	Resolved     string                           `json:"resolved"`
	Dependencies map[string]packageLockDependency `json:"dependencies"`
}

func parsePackageLock(source Source, opts ParseOptions) ([]Artifact, error) {
	alias, err := aliasFor(opts, ecosystem.NPM)
	if err != nil {
		return nil, err
	}
	var payload packageLock
	if err := json.Unmarshal(source.Body, &payload); err != nil {
		return nil, oops.In("dependency-prefetch").Wrapf(err, "decode package-lock.json")
	}
	artifacts := make([]Artifact, 0, len(payload.Packages)+len(payload.Dependencies))
	for _, path := range sortedKeys(payload.Packages) {
		pkg := payload.Packages[path]
		name, ok := packageNameFromLockPath(path)
		if !ok {
			continue
		}
		if artifact, ok := npmTarballArtifact(source, opts, alias, name, pkg.Resolved, 0); ok {
			artifacts = append(artifacts, artifact)
		}
	}
	walkPackageLockDependencies(source, opts, alias, payload.Dependencies, 0, &artifacts)
	return dedupeArtifacts(artifacts), nil
}

func walkPackageLockDependencies(
	source Source,
	opts ParseOptions,
	alias string,
	dependencies map[string]packageLockDependency,
	depth int,
	artifacts *[]Artifact,
) {
	for _, name := range sortedKeys(dependencies) {
		dep := dependencies[name]
		if artifact, ok := npmTarballArtifact(source, opts, alias, name, dep.Resolved, 0); ok {
			*artifacts = append(*artifacts, artifact)
		}
		if depth < 8 {
			walkPackageLockDependencies(source, opts, alias, dep.Dependencies, depth+1, artifacts)
		}
	}
}

func npmTarballArtifact(source Source, opts ParseOptions, alias, name, resolved string, line int) (Artifact, bool) {
	name = strings.TrimSpace(name)
	tarball, ok := tarballName(resolved)
	if name == "" || !ok {
		return Artifact{}, false
	}
	return withDefaults(Artifact{
		Ecosystem: ecosystem.NPM,
		Alias:     alias,
		Artifact:  name,
		Reference: "tarball:" + tarball,
		Line:      line,
	}, source, opts), true
}

func packageNameFromLockPath(path string) (string, bool) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return "", false
	}
	segments := strings.Split(path, "/")
	index := lastNodeModulesIndex(segments)
	if index < 0 || index+1 >= len(segments) {
		return "", false
	}
	return packageNameFromSegments(segments[index+1:])
}

func lastNodeModulesIndex(segments []string) int {
	index := -1
	for i, segment := range segments {
		if segment == "node_modules" {
			index = i
		}
	}
	return index
}

func packageNameFromSegments(segments []string) (string, bool) {
	name := segments[0]
	if name == "" {
		return "", false
	}
	if !strings.HasPrefix(name, "@") {
		return name, true
	}
	if len(segments) < 2 || segments[1] == "" {
		return "", false
	}
	return name + "/" + segments[1], true
}

func tarballName(resolved string) (string, bool) {
	resolved = strings.TrimSpace(resolved)
	if resolved == "" {
		return "", false
	}
	u, err := url.Parse(resolved)
	if err != nil {
		return "", false
	}
	path := u.Path
	if path == "" {
		path = resolved
	}
	_, file, ok := strings.Cut(path, "/-/")
	if !ok {
		return "", false
	}
	file = strings.TrimSpace(file)
	if file == "" || strings.Contains(file, "/") || !strings.HasSuffix(file, ".tgz") {
		return "", false
	}
	unescaped, err := url.PathUnescape(file)
	if err == nil {
		file = unescaped
	}
	return file, true
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
