package prefetch

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/manualsync"
	"github.com/samber/oops"
)

func Parse(source Source, opts ParseOptions) ([]Artifact, error) {
	format := sourceFormat(source)
	switch format {
	case FormatGoSum:
		return parseGoSum(source, opts)
	case FormatPackageLock:
		return parsePackageLock(source, opts)
	case FormatRequirements:
		return parseRequirements(source, opts)
	case FormatPOM:
		return parsePOM(source, opts)
	case FormatOCIManifest:
		return parseOCIManifest(source, opts)
	case FormatContainerRefs:
		return parseContainerRefs(source, opts)
	default:
		return nil, oops.In("dependency-prefetch").With("format", format).Errorf("unsupported dependency manifest format")
	}
}

func sourceFormat(source Source) string {
	if source.Format != FormatAuto {
		return strings.TrimSpace(source.Format)
	}
	if format := sourceNameFormat(source.Name); format != "" {
		return format
	}
	return sourceBodyFormat(source.Body)
}

func sourceNameFormat(name string) string {
	name = strings.ToLower(filepath.Base(strings.TrimSpace(name)))
	switch name {
	case "go.sum":
		return FormatGoSum
	case "package-lock.json", "npm-shrinkwrap.json":
		return FormatPackageLock
	case "requirements.txt":
		return FormatRequirements
	case "pom.xml":
		return FormatPOM
	default:
		return ""
	}
}

func sourceBodyFormat(raw []byte) string {
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return FormatContainerRefs
	}
	if looksJSON(body) {
		if looksPackageLock(raw) {
			return FormatPackageLock
		}
		return FormatOCIManifest
	}
	if looksGoSum(body) {
		return FormatGoSum
	}
	if looksPOM(body) {
		return FormatPOM
	}
	if looksRequirements(body) {
		return FormatRequirements
	}
	return FormatContainerRefs
}

func aliasFor(opts ParseOptions, ecosystemName string) (string, error) {
	alias := strings.TrimSpace(opts.DefaultAliases[ecosystemName])
	if alias == "" {
		return "", oops.In("dependency-prefetch").With("ecosystem", ecosystemName).Errorf("default upstream alias is required")
	}
	return alias, nil
}

func withDefaults(artifact Artifact, source Source, opts ParseOptions) Artifact {
	artifact.Source = source.Name
	if artifact.Accept == "" {
		artifact.Accept = opts.Accept
	}
	return artifact
}

func dedupeArtifacts(artifacts []Artifact) []Artifact {
	seen := map[string]struct{}{}
	out := make([]Artifact, 0, len(artifacts))
	for i := range artifacts {
		artifact := artifacts[i]
		key := strings.Join([]string{
			artifact.Ecosystem,
			artifact.Alias,
			artifact.Artifact,
			artifact.Reference,
			artifact.Accept,
		}, "\x1f")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, artifact)
	}
	return out
}

func looksJSON(body string) bool {
	return strings.HasPrefix(body, "{") || strings.HasPrefix(body, "[")
}

func looksPackageLock(body []byte) bool {
	var payload struct {
		LockfileVersion any `json:"lockfileVersion"`
		Packages        any `json:"packages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return payload.LockfileVersion != nil || payload.Packages != nil
}

func looksGoSum(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && strings.HasPrefix(fields[2], "h1:") {
			return true
		}
	}
	return false
}

func looksPOM(body string) bool {
	return strings.Contains(body, "<project") && strings.Contains(body, "<dependencies")
}

func looksRequirements(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		if strings.Contains(line, "==") || strings.Contains(line, "#egg=") {
			return true
		}
	}
	return false
}

func syncOptions(artifact Artifact) manualsync.SyncOptions {
	return manualsync.SyncOptions{
		Ecosystem: artifact.Ecosystem,
		Alias:     artifact.Alias,
		Artifact:  artifact.Artifact,
		Reference: artifact.Reference,
		Accept:    artifact.Accept,
	}
}
