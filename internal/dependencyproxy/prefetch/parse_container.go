package prefetch

import (
	"encoding/json"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	containerreference "github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/samber/oops"
)

type ociCollector struct {
	source    Source
	opts      ParseOptions
	artifacts []Artifact
}

type containerTarget struct {
	Alias      string
	Repository string
	Reference  string
}

func parseContainerRefs(source Source, opts ParseOptions) ([]Artifact, error) {
	artifacts := make([]Artifact, 0)
	lineNo := 0
	for line := range strings.SplitSeq(string(source.Body), "\n") {
		lineNo++
		line = cleanContainerRefLine(line)
		if line == "" {
			continue
		}
		target, err := parseContainerRefString(line, opts)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, containerArtifact(source, opts, target, lineNo))
	}
	return dedupeArtifacts(artifacts), nil
}

func parseOCIManifest(source Source, opts ParseOptions) ([]Artifact, error) {
	var payload any
	if err := json.Unmarshal(source.Body, &payload); err != nil {
		return nil, oops.In("dependency-prefetch").Wrapf(err, "decode OCI manifest descriptors")
	}
	collector := ociCollector{source: source, opts: opts}
	if err := collector.collect(payload, containerTarget{}); err != nil {
		return nil, err
	}
	return dedupeArtifacts(collector.artifacts), nil
}

func (c *ociCollector) collect(value any, inherited containerTarget) error {
	switch typed := value.(type) {
	case []any:
		return c.collectList(typed, inherited)
	case map[string]any:
		return c.collectMap(typed, inherited)
	}
	return nil
}

func (c *ociCollector) collectList(values []any, inherited containerTarget) error {
	for i := range values {
		if err := c.collect(values[i], inherited); err != nil {
			return err
		}
	}
	return nil
}

func (c *ociCollector) collectMap(values map[string]any, inherited containerTarget) error {
	target := inheritedContainerTarget(c.opts, inherited, values)
	if err := c.collectImageRef(values); err != nil {
		return err
	}
	if err := c.collectDescriptor(values, target); err != nil {
		return err
	}
	return c.collectChildren(values, target)
}

func (c *ociCollector) collectImageRef(values map[string]any) error {
	imageRef := firstString(values, "image", "imageRef", "container", "containerRef")
	if imageRef == "" {
		return nil
	}
	parsed, err := parseContainerRefString(imageRef, c.opts)
	if err != nil {
		return err
	}
	c.artifacts = append(c.artifacts, containerArtifact(c.source, c.opts, parsed, 0))
	return nil
}

func (c *ociCollector) collectDescriptor(values map[string]any, target containerTarget) error {
	if target.Repository == "" {
		return nil
	}
	if digest := firstString(values, "digest"); digest != "" {
		target.Reference = digest
	}
	if target.Reference == "" {
		return nil
	}
	if target.Alias == "" {
		return oops.In("dependency-prefetch").With("repository", target.Repository).Errorf("container descriptor upstream alias is required")
	}
	c.artifacts = append(c.artifacts, containerArtifact(c.source, c.opts, target, 0))
	return nil
}

func (c *ociCollector) collectChildren(values map[string]any, target containerTarget) error {
	for _, key := range []string{"manifests", "descriptors", "children"} {
		child, ok := values[key]
		if !ok {
			continue
		}
		if err := c.collect(child, target); err != nil {
			return err
		}
	}
	return nil
}

func inheritedContainerTarget(opts ParseOptions, inherited containerTarget, values map[string]any) containerTarget {
	target := inherited
	if alias := firstString(values, "alias", "upstreamAlias"); alias != "" {
		target.Alias = alias
	}
	if target.Alias == "" {
		target.Alias = strings.TrimSpace(opts.DefaultAliases[ecosystem.Container])
	}
	if repo := firstString(values, "repository", "repo", "artifact"); repo != "" {
		target.Repository = strings.Trim(repo, "/")
	}
	if ref := firstString(values, "reference", "tag"); ref != "" {
		target.Reference = ref
	}
	if target.Reference == "" {
		if annotations, ok := values["annotations"].(map[string]any); ok {
			target.Reference = firstString(annotations, "org.opencontainers.image.ref.name")
		}
	}
	return target
}

func parseContainerRefString(value string, opts ParseOptions) (containerTarget, error) {
	value = cleanContainerRefLine(value)
	if value == "" {
		return containerTarget{}, oops.In("dependency-prefetch").Errorf("container image reference is required")
	}
	if isContainerManifestPath(value) {
		return parseContainerManifestPath(value)
	}

	alias, refValue, err := containerRefAlias(value, opts)
	if err != nil {
		return containerTarget{}, err
	}
	repo, ref, err := splitContainerRepositoryReference(refValue)
	if err != nil {
		return containerTarget{}, err
	}
	return containerTarget{Alias: alias, Repository: repo, Reference: ref}, nil
}

func isContainerManifestPath(value string) bool {
	return strings.HasPrefix(value, "/v2/") || strings.HasPrefix(value, "v2/")
}

func parseContainerManifestPath(value string) (containerTarget, error) {
	path := value
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	route, err := containerreference.ParseManifestPath(path)
	if err != nil {
		return containerTarget{}, oops.In("dependency-prefetch").Wrapf(err, "parse container manifest path")
	}
	return containerTarget{Alias: route.Alias, Repository: route.Repo, Reference: route.Reference}, nil
}

func containerRefAlias(value string, opts ParseOptions) (string, string, error) {
	explicitAlias := false
	if rest, ok := strings.CutPrefix(value, "container:"); ok {
		value = rest
		explicitAlias = true
	}
	alias := strings.TrimSpace(opts.DefaultAliases[ecosystem.Container])
	if !explicitAlias && alias != "" {
		return alias, value, nil
	}
	first, rest, ok := strings.Cut(value, "/")
	if !ok || first == "" || rest == "" {
		return "", "", oops.In("dependency-prefetch").With("reference", value).Errorf("container reference must include upstream alias")
	}
	return first, rest, nil
}

func splitContainerRepositoryReference(value string) (string, string, error) {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return "", "", oops.In("dependency-prefetch").With("reference", value).Errorf("container reference is invalid")
	}
	if repo, ref, ok := strings.Cut(value, "@"); ok {
		if repo == "" || ref == "" {
			return "", "", oops.In("dependency-prefetch").With("reference", value).Errorf("container digest reference is invalid")
		}
		return repo, ref, nil
	}
	colon := strings.LastIndex(value, ":")
	slash := strings.LastIndex(value, "/")
	if colon > slash {
		repo := value[:colon]
		ref := value[colon+1:]
		if repo == "" || ref == "" {
			return "", "", oops.In("dependency-prefetch").With("reference", value).Errorf("container tag reference is invalid")
		}
		return repo, ref, nil
	}
	return value, "latest", nil
}

func containerArtifact(source Source, opts ParseOptions, target containerTarget, line int) Artifact {
	return withDefaults(Artifact{
		Ecosystem: ecosystem.Container,
		Alias:     strings.TrimSpace(target.Alias),
		Artifact:  strings.Trim(target.Repository, "/"),
		Reference: strings.TrimSpace(target.Reference),
		Line:      line,
	}, source, opts)
}

func cleanContainerRefLine(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "- ")
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "#") {
		return ""
	}
	if value, ok := strings.CutPrefix(line, "image:"); ok {
		line = strings.TrimSpace(value)
	}
	return strings.Trim(line, `"'`)
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}
