package dockerintegration

import (
	"strings"

	distributionreference "github.com/distribution/reference"
	"github.com/samber/oops"
)

type ProxyReferenceOptions struct {
	Registry         string
	Alias            string
	DefaultNamespace string
}

func BuildProxyReference(opts ProxyReferenceOptions, image string) (string, error) {
	registry := strings.TrimRight(strings.TrimSpace(opts.Registry), "/")
	alias := strings.Trim(strings.TrimSpace(opts.Alias), "/")
	if registry == "" {
		return "", oops.In("docker").Errorf("proxy registry is required")
	}
	if alias == "" {
		return "", oops.In("docker").Errorf("proxy upstream alias is required")
	}

	repository, suffix, err := splitImageReference(image, opts.DefaultNamespace)
	if err != nil {
		return "", err
	}
	return registry + "/" + alias + "/" + repository + suffix, nil
}

func splitImageReference(image, defaultNamespace string) (string, string, error) {
	name, suffix := splitReferenceSuffix(strings.TrimSpace(image))
	name = stripRegistryDomain(strings.Trim(name, "/"))
	if name == "" {
		return "", "", oops.In("docker").Errorf("image reference is required")
	}
	if !strings.Contains(name, "/") {
		namespace := strings.Trim(defaultNamespace, "/")
		if namespace != "" {
			name = namespace + "/" + name
		}
	}
	if suffix == "" {
		suffix = ":latest"
	}
	if _, err := distributionreference.ParseNormalizedNamed(name + suffix); err != nil {
		return "", "", oops.In("docker").With("image", image).Wrapf(err, "parse image reference")
	}
	if _, err := distributionreference.WithName(name); err != nil {
		return "", "", oops.In("docker").With("repository", name).Wrapf(err, "validate image repository")
	}
	return name, suffix, nil
}

func splitReferenceSuffix(image string) (string, string) {
	name, digest := splitDigestSuffix(image)
	name, tag := splitTagSuffix(name)
	return name, tag + digest
}

func splitDigestSuffix(image string) (string, string) {
	name, digest, ok := strings.Cut(image, "@")
	if !ok {
		return image, ""
	}
	return name, "@" + digest
}

func splitTagSuffix(name string) (string, string) {
	lastSlash := strings.LastIndex(name, "/")
	lastColon := strings.LastIndex(name, ":")
	if lastColon <= lastSlash {
		return name, ""
	}
	return name[:lastColon], name[lastColon:]
}

func stripRegistryDomain(name string) string {
	first, rest, ok := strings.Cut(name, "/")
	if !ok {
		return name
	}
	if first == "localhost" || strings.ContainsAny(first, ".:") {
		return rest
	}
	return name
}
