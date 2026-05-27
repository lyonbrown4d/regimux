package config

import (
	"fmt"
	"strings"

	"github.com/samber/oops"
)

func normalizeUpstreamConfig(alias string, upstreamCfg UpstreamConfig) (UpstreamConfig, error) {
	if strings.TrimSpace(alias) == "" {
		return UpstreamConfig{}, oops.In("config").Errorf("upstream alias cannot be empty")
	}
	upstreamCfg.Alias = alias
	upstreamCfg.Registry = strings.TrimSpace(upstreamCfg.Registry)

	policy, policyErr := normalizeMirrorPolicy(alias, upstreamCfg.MirrorPolicy)
	if policyErr != nil {
		return UpstreamConfig{}, policyErr
	}
	upstreamCfg.MirrorPolicy = policy

	var blobErr error
	upstreamCfg.Blob, blobErr = normalizeUpstreamBlobConfig(alias, policy, upstreamCfg.Blob)
	if blobErr != nil {
		return UpstreamConfig{}, blobErr
	}
	var probeErr error
	upstreamCfg.Probe, probeErr = normalizeUpstreamProbeConfig(alias, upstreamCfg.Probe)
	if probeErr != nil {
		return UpstreamConfig{}, probeErr
	}
	if upstreamCfg.Blob.MirrorPolicy == "latency" {
		upstreamCfg.Probe.Enabled = true
	}

	if sourceErr := validateUpstreamSource(alias, upstreamCfg); sourceErr != nil {
		return UpstreamConfig{}, sourceErr
	}
	mirrors, err := normalizeMirrors(alias, upstreamCfg.Mirrors)
	if err != nil {
		return UpstreamConfig{}, err
	}
	upstreamCfg.Mirrors = mirrors
	if upstreamCfg.Auth.Type == "" {
		upstreamCfg.Auth.Type = "anonymous"
	}
	return upstreamCfg, nil
}

func normalizeMirrorPolicy(alias, policy string) (string, error) {
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy == "" || policy == "failover" {
		return "ordered", nil
	}
	switch policy {
	case "ordered", "round_robin":
		return policy, nil
	default:
		return "", oops.In("config").With("alias", alias, "mirror_policy", policy).Errorf("upstreams.%s.mirror_policy must be ordered or round_robin", alias)
	}
}

func normalizeUpstreamBlobConfig(alias, upstreamPolicy string, blobCfg UpstreamBlobConfig) (UpstreamBlobConfig, error) {
	if blobCfg.TopN < 0 {
		return UpstreamBlobConfig{}, oops.In("config").With("alias", alias).Errorf("upstreams.%s.blob.top_n cannot be negative", alias)
	}
	if blobCfg.MaxConcurrencyPerEndpoint < 0 {
		return UpstreamBlobConfig{}, oops.In("config").With("alias", alias).Errorf("upstreams.%s.blob.max_concurrency_per_endpoint cannot be negative", alias)
	}
	if blobCfg.MaxConcurrentAttempts < 0 {
		return UpstreamBlobConfig{}, oops.In("config").With("alias", alias).Errorf("upstreams.%s.blob.max_concurrent_attempts cannot be negative", alias)
	}

	policy, err := normalizeBlobMirrorPolicy(alias, blobCfg.MirrorPolicy, upstreamPolicy)
	if err != nil {
		return UpstreamBlobConfig{}, err
	}
	blobCfg.MirrorPolicy = policy
	if blobCfg.TopN == 0 {
		blobCfg.TopN = defaultUpstreamBlobTopN
	}
	if blobCfg.MaxConcurrentAttempts == 0 {
		blobCfg.MaxConcurrentAttempts = defaultUpstreamBlobMaxAttempts
	}
	return blobCfg, nil
}

func normalizeBlobMirrorPolicy(alias, policy, upstreamPolicy string) (string, error) {
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy == "" {
		if upstreamPolicy == "" {
			return "ordered", nil
		}
		return upstreamPolicy, nil
	}
	switch policy {
	case "ordered", "round_robin", "latency":
		return policy, nil
	default:
		return "", oops.In("config").With("alias", alias, "blob_mirror_policy", policy).Errorf("upstreams.%s.blob.mirror_policy must be ordered, round_robin, or latency", alias)
	}
}

func normalizeUpstreamProbeConfig(alias string, probeCfg UpstreamProbeConfig) (UpstreamProbeConfig, error) {
	checks := []struct {
		invalid bool
		err     error
	}{
		{probeCfg.Interval < 0, oops.In("config").With("alias", alias).Errorf("upstreams.%s.probe.interval cannot be negative", alias)},
		{probeCfg.Timeout < 0, oops.In("config").With("alias", alias).Errorf("upstreams.%s.probe.timeout cannot be negative", alias)},
		{probeCfg.Cooldown < 0, oops.In("config").With("alias", alias).Errorf("upstreams.%s.probe.cooldown cannot be negative", alias)},
	}
	for _, check := range checks {
		if check.invalid {
			return UpstreamProbeConfig{}, check.err
		}
	}
	if probeCfg.Interval == 0 {
		probeCfg.Interval = defaultUpstreamProbeInterval
	}
	if probeCfg.Timeout == 0 {
		probeCfg.Timeout = defaultUpstreamProbeTimeout
	}
	if probeCfg.Cooldown == 0 {
		probeCfg.Cooldown = defaultUpstreamProbeCooldown
	}
	return probeCfg, nil
}

func validateUpstreamSource(alias string, upstreamCfg UpstreamConfig) error {
	if upstreamCfg.Registry == "" && len(upstreamCfg.Mirrors) == 0 {
		return oops.In("config").With("alias", alias).Errorf("upstreams.%s.registry or upstreams.%s.mirrors is required", alias, alias)
	}
	if upstreamCfg.Registry == "" {
		return nil
	}
	return validateURL("upstreams."+alias+".registry", upstreamCfg.Registry)
}

func normalizeMirrors(alias string, mirrors []string) ([]string, error) {
	for i, mirror := range mirrors {
		mirror = strings.TrimSpace(mirror)
		if err := validateURL(fmt.Sprintf("upstreams.%s.mirrors[%d]", alias, i), mirror); err != nil {
			return nil, err
		}
		mirrors[i] = mirror
	}
	return uniqueStrings(mirrors), nil
}
