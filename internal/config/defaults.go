package config

import "time"

func defaultConfig() Config {
	return Config{
		Server:    defaultServerConfig(),
		Auth:      defaultRegistryAuthConfig(),
		Log:       defaultLogConfig(),
		Cache:     defaultCacheConfig(),
		Store:     defaultStoreConfig(),
		Scheduler: defaultSchedulerConfig(),
		Worker:    defaultWorkerConfig(),
		Upstreams: defaultUpstreamsConfig(),
	}
}

func defaultRegistryAuthConfig() RegistryAuthConfig {
	return RegistryAuthConfig{
		Service:  "regimux",
		Issuer:   "regimux",
		TokenTTL: 15 * time.Minute,
	}
}

func defaultServerConfig() ServerConfig {
	return ServerConfig{
		Listen:       ":5000",
		PublicURL:    "http://localhost:5000",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
		Middleware:   defaultServerMiddlewareConfig(),
	}
}

func defaultServerMiddlewareConfig() ServerMiddlewareConfig {
	return ServerMiddlewareConfig{
		RequestID: MiddlewareRequestIDConfig{
			Enabled: true,
			Header:  "X-Request-ID",
		},
		Healthcheck: MiddlewareHealthcheckConfig{
			Enabled:       true,
			LivenessPath:  "/livez",
			ReadinessPath: "/readyz",
		},
		ETag:            MiddlewareToggleConfig{Enabled: true},
		SecurityHeaders: MiddlewareSecurityHeadersConfig{Enabled: true},
		Compress: MiddlewareCompressConfig{
			Enabled: true,
			Level:   "default",
		},
		RateLimit: MiddlewareRateLimitConfig{
			Max:        60,
			Expiration: time.Minute,
		},
		CSRF: MiddlewareCSRFConfig{
			IdleTimeout: 30 * time.Minute,
			CookieName:  "regimux_csrf",
		},
	}
}

func defaultLogConfig() LogConfig {
	return LogConfig{
		Level:      "info",
		Console:    true,
		AddCaller:  false,
		MaxSizeMB:  100,
		MaxAgeDays: 7,
		MaxBackups: 10,
		TimeFormat: "2006-01-02 15:04:05",
		SetDefault: true,
		LocalTime:  true,
		Compress:   true,
	}
}

func defaultCacheConfig() CacheConfig {
	return CacheConfig{
		Backend:    "memory",
		Prefix:     "regimux",
		DefaultTTL: 10 * time.Minute,
		Memory:     MemoryCacheConfig{MaxItems: 10000},
		Redis:      ExternalCacheConfig{Addrs: []string{"127.0.0.1:6379"}, DB: 0},
		Valkey:     ExternalCacheConfig{Addrs: []string{"127.0.0.1:6379"}, DB: 0},
		Manifest:   ManifestCacheConfig{TagTTL: 10 * time.Minute, StaleIfError: true, MaxStale: 168 * time.Hour},
		Blob:       BlobCacheConfig{VerifyTTL: 0, StreamAndCache: false},
		Tags:       TagsCacheConfig{TTL: 5 * time.Minute, MaxPageSize: 1000},
		Referrers:  ReferrersConfig{TTL: 5 * time.Minute, FallbackTag: true},
	}
}

func defaultStoreConfig() StoreConfig {
	return StoreConfig{
		Meta:   StoreMetaConfig{Driver: "sqlite", Path: "data/regimux.db"},
		Object: StoreObjectConfig{Driver: "local", Path: "data/objects"},
	}
}

func defaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		Enabled:         true,
		DistributedLock: true,
		LockTTL:         5 * time.Minute,
		Cleanup: SchedulerCleanupConfig{
			Enabled:    true,
			Interval:   time.Hour,
			UnusedFor:  168 * time.Hour,
			MaxDeletes: 1000,
		},
		Prefetch: SchedulerPrefetchConfig{
			Interval:             30 * time.Minute,
			MaxRecords:           200,
			MinPullCount:         2,
			TagsPageSize:         1000,
			MaxCandidatesPerRepo: 3,
			MaxVersionDistance:   5,
			FailureBackoff:       time.Hour,
			RetryWindow:          24 * time.Hour,
			Distributed:          true,
		},
	}
}

func defaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		ProbeConcurrency:    16,
		PrefetchConcurrency: 8,
	}
}

func defaultUpstreamsConfig() map[string]UpstreamConfig {
	return map[string]UpstreamConfig{"hub": defaultHubUpstreamConfig()}
}

func defaultHubUpstreamConfig() UpstreamConfig {
	return UpstreamConfig{
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

func DefaultConfig() Config {
	return defaultConfig()
}
