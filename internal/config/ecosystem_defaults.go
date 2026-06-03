package config

import "time"

func defaultContainerConfig() ContainerConfig {
	return ContainerConfig{
		"hub":  containerRegistryFromUpstreamConfig(defaultHubUpstreamConfig()),
		"ghcr": containerRegistryFromUpstreamConfig(defaultGHCRUpstreamConfig()),
		"quay": containerRegistryFromUpstreamConfig(defaultQuayUpstreamConfig()),
	}
}

func defaultGoConfig() DependencyEcosystemConfig {
	return DependencyEcosystemConfig{
		"default": dependencyUpstreamFromUpstreamConfig(defaultGoUpstreamConfig()),
	}
}

func defaultMavenConfig() DependencyEcosystemConfig {
	return DependencyEcosystemConfig{
		"central": dependencyUpstreamFromUpstreamConfig(defaultMavenUpstreamConfig()),
	}
}

func defaultPyPIConfig() DependencyEcosystemConfig {
	return DependencyEcosystemConfig{
		"default": dependencyUpstreamFromUpstreamConfig(defaultPyPIUpstreamConfig()),
	}
}

func defaultNPMConfig() DependencyEcosystemConfig {
	return DependencyEcosystemConfig{
		"default": dependencyUpstreamFromUpstreamConfig(defaultNPMUpstreamConfig()),
	}
}

func defaultHubUpstreamConfig() UpstreamConfig {
	return UpstreamConfig{
		Type:             "oci",
		Registry:         "https://registry-1.docker.io",
		MirrorPolicy:     "ordered",
		DefaultNamespace: "library",
		TagTTL:           10 * time.Minute,
		Blob: UpstreamBlobConfig{
			MirrorPolicy:          "ordered",
			TopN:                  3,
			MaxConcurrentAttempts: 1,
		},
		Probe: UpstreamProbeConfig{
			Interval: 30 * time.Second,
			Timeout:  3 * time.Second,
			Cooldown: 2 * time.Minute,
			Jitter:   5 * time.Second,
		},
		Auth: AuthConfig{Type: "anonymous"},
		HTTP: HTTPConfig{
			Retry: HTTPRetryConfig{
				Enabled:    true,
				MaxRetries: 2,
				WaitMin:    100 * time.Millisecond,
				WaitMax:    time.Second,
			},
		},
	}
}

func defaultGHCRUpstreamConfig() UpstreamConfig {
	upstreamCfg := defaultHubUpstreamConfig()
	upstreamCfg.Registry = "https://ghcr.io"
	upstreamCfg.DefaultNamespace = ""
	upstreamCfg.TagTTL = 5 * time.Minute
	upstreamCfg.Blob = UpstreamBlobConfig{}
	upstreamCfg.Probe = UpstreamProbeConfig{}
	return upstreamCfg
}

func defaultQuayUpstreamConfig() UpstreamConfig {
	upstreamCfg := defaultGHCRUpstreamConfig()
	upstreamCfg.Registry = "https://quay.io"
	return upstreamCfg
}

func defaultGoUpstreamConfig() UpstreamConfig {
	return dependencyDefaultUpstream("go", "https://proxy.golang.org")
}

func defaultMavenUpstreamConfig() UpstreamConfig {
	return dependencyDefaultUpstream("maven", "https://repo.maven.apache.org/maven2")
}

func defaultPyPIUpstreamConfig() UpstreamConfig {
	return dependencyDefaultUpstream("pypi", "https://pypi.org")
}

func defaultNPMUpstreamConfig() UpstreamConfig {
	return dependencyDefaultUpstream("npm", "https://registry.npmjs.org")
}

func dependencyDefaultUpstream(ecosystem, registry string) UpstreamConfig {
	return UpstreamConfig{
		Type:     ecosystem,
		Registry: registry,
		Auth:     AuthConfig{Type: "anonymous"},
		HTTP: HTTPConfig{
			Retry: HTTPRetryConfig{
				Enabled:    true,
				MaxRetries: 2,
				WaitMin:    100 * time.Millisecond,
				WaitMax:    time.Second,
			},
		},
	}
}
