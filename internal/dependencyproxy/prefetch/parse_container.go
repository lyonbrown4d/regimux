package prefetch

import (
	"encoding/json"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	containerreference "github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/samber/oops"
)

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
	artifacts := make([]Artifact, 0)
	if err := collectOCIArtifacts(source, opts, payload, containerTarget{}, &artifacts); err != nil {
		return nil, err
	}
	return dedupeArtifacts(artifacts), nil
}

func collectOCIArtifacts(source Source, opts ParseOptions, value any, inherited containerTarget, artifacts *[]Artifact) error {
	switch typed := value.(type) {
	case []any:
		for i := range typed {
			if err := collectOCIArtifacts(source, opts, typed[i], inherited, artifacts); err != nil {
				return err
			}
		}
	case map[string]any:
		target := inheritedContainerTarget(opts, inherited, typed)
		if imageRef := firstString(typed, "image", "imageRef", "container", "containerRef"); imageRef != "" {
			parsed, err := parseContainerRefString(imageRef, opts)
			if err != nil {
				return err
			}
			*artifacts = append(*artifacts, containerArtifact(source, opts, parsed, 0))
		}
		if target.Repository != "" {
			if digest := firstString(typed, "digest"); digest != "" {
				target.Reference = digest
			}
			if target.Reference != "" {
				if target.Alias == "" {
					return oops.In("dependency-prefetch").With("repository", target.Repository).Errorf("container descriptor upstream alias is required")
				}
				*artifacts = append(*artifacts, containerArtifact(source, opts, target, 0))
			}
		}
		for _, key := range []string{"manifests", "descriptors", "children"} {
			child, ok := typed[key]
			if !ok {
				continue
			}
			if err := collectOCIArtifacts(source, opts, child, target, artifacts); err != nil {
				return err
			}
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
	if strings.HasPrefix(value, "/v2/") || strings.HasPrefix(value, "v2/") {
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

	explicitAlias := false
	if rest, ok := strings.CutPrefix(value, "container:"); ok {
		value = rest
		explicitAlias = true
	}
	alias := strings.TrimSpace(opts.DefaultAliases[ecosystem.Container])
	if explicitAlias || alias == "" {
		first, rest, ok := strings.Cut(value, "/")
		if !ok || first == "" || rest == "" {
			return containerTarget{}, oops.In("dependency-prefetch").With("reference", value).Errorf("container reference must include upstream alias")
		}
		alias = first
		value = rest
	}
	repo, ref, err := splitContainerRepositoryReference(value)
	if err != nil {
		return containerTarget{}, err
	}
	return containerTarget{Alias: alias, Repository: repo, Reference: ref}, nil
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
