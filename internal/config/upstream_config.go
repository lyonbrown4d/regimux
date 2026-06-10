package config

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

func normalizeUpstreamConfig(alias string, upstreamCfg UpstreamConfig) (UpstreamConfig, error) {
	if strings.TrimSpace(alias) == "" {
		return UpstreamConfig{}, oops.In("config").Errorf("upstream alias cannot be empty")
	}
	upstreamCfg.Alias = alias
	upstreamCfg.Type = normalizeUpstreamType(upstreamCfg.Type)
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

func normalizeUpstreamType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "oci"
	}
	return value
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
	blobCfg.TopN = lo.CoalesceOrEmpty(blobCfg.TopN, defaultUpstreamBlobTopN)
	blobCfg.MaxConcurrentAttempts = lo.CoalesceOrEmpty(blobCfg.MaxConcurrentAttempts, defaultUpstreamBlobMaxAttempts)
	return blobCfg, nil
}

func normalizeBlobMirrorPolicy(alias, policy, upstreamPolicy string) (string, error) {
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy == "" {
		return lo.CoalesceOrEmpty(upstreamPolicy, "ordered"), nil
	}
	switch policy {
	case "ordered", "round_robin", "latency":
		return policy, nil
	default:
		return "", oops.In("config").With("alias", alias, "blob_mirror_policy", policy).Errorf("upstreams.%s.blob.mirror_policy must be ordered, round_robin, or latency", alias)
	}
}

func normalizeUpstreamProbeConfig(alias string, probeCfg UpstreamProbeConfig) (UpstreamProbeConfig, error) {
	type validationCheck struct {
		invalid bool
		err     error
	}
	checks := []validationCheck{
		{probeCfg.Interval < 0, oops.In("config").With("alias", alias).Errorf("upstreams.%s.probe.interval cannot be negative", alias)},
		{probeCfg.Timeout < 0, oops.In("config").With("alias", alias).Errorf("upstreams.%s.probe.timeout cannot be negative", alias)},
		{probeCfg.Cooldown < 0, oops.In("config").With("alias", alias).Errorf("upstreams.%s.probe.cooldown cannot be negative", alias)},
		{probeCfg.Jitter < 0, oops.In("config").With("alias", alias).Errorf("upstreams.%s.probe.jitter cannot be negative", alias)},
	}
	if check, ok := lo.Find(checks, func(check validationCheck) bool {
		return check.invalid
	}); ok {
		return UpstreamProbeConfig{}, check.err
	}
	probeCfg.Interval = lo.CoalesceOrEmpty(probeCfg.Interval, defaultUpstreamProbeInterval)
	probeCfg.Timeout = lo.CoalesceOrEmpty(probeCfg.Timeout, defaultUpstreamProbeTimeout)
	probeCfg.Cooldown = lo.CoalesceOrEmpty(probeCfg.Cooldown, defaultUpstreamProbeCooldown)
	probeCfg.Jitter = lo.CoalesceOrEmpty(probeCfg.Jitter, defaultUpstreamProbeJitter)
	if probeCfg.Interval > 0 && probeCfg.Jitter > probeCfg.Interval {
		probeCfg.Jitter = probeCfg.Interval
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
	normalized, err := lo.MapErr(mirrors, func(mirror string, i int) (string, error) {
		mirror = strings.TrimSpace(mirror)
		if err := validateURL(fmt.Sprintf("upstreams.%s.mirrors[%d]", alias, i), mirror); err != nil {
			return "", err
		}
		return mirror, nil
	})
	if err != nil {
		return nil, oops.In("config").With("alias", alias).Wrapf(err, "normalize upstream mirrors")
	}
	return uniqueStrings(normalized), nil
}
