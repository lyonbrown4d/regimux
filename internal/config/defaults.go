package config

import "time"

func defaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Listen:       ":5000",
			PublicURL:    "http://localhost:5000",
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 0,
			IdleTimeout:  120 * time.Second,
		},
		Log: LogConfig{
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
		},
		Cache: CacheConfig{
			Backend:     "memory",
			Prefix:      "regimux",
			DefaultTTL:   10 * time.Minute,
			Memory:      MemoryCacheConfig{MaxItems: 10000},
			Redis:       ExternalCacheConfig{Addrs: []string{"127.0.0.1:6379"}, DB: 0},
			Valkey:      ExternalCacheConfig{Addrs: []string{"127.0.0.1:6379"}, DB: 0},
			Manifest:    ManifestCacheConfig{TagTTL: 10 * time.Minute, StaleIfError: true, MaxStale: 168 * time.Hour},
			Blob:        BlobCacheConfig{VerifyTTL: 0, StreamAndCache: false},
			Tags:        TagsCacheConfig{TTL: 5 * time.Minute, MaxPageSize: 1000},
			Referrers:   ReferrersConfig{TTL: 5 * time.Minute, FallbackTag: true},
		},
		Store: StoreConfig{
			Meta:   StoreMetaConfig{Driver: "bboltx", Path: "data/regimux.db"},
			Object: StoreObjectConfig{Driver: "local", Path: "data/objects"},
		},
		Scheduler: SchedulerConfig{
			Enabled:         true,
			DistributedLock: true,
			LockTTL:         5 * time.Minute,
			Cleanup: SchedulerCleanupConfig{
				Enabled:     true,
				Interval:    time.Hour,
				MaxScan:     0,
				UnusedFor:   168 * time.Hour,
				MaxDeletes:  1000,
				DryRun:      false,
				Distributed: false,
			},
			Prefetch: SchedulerPrefetchConfig{
				Enabled:            false,
				Interval:           30 * time.Minute,
				MaxRecords:         200,
				MinPullCount:       2,
				TagsPageSize:       1000,
				MaxCandidatesPerRepo: 3,
				MaxVersionDistance:  5,
				Distributed:        true,
			},
		},
		Worker: WorkerConfig{
			ProbeConcurrency:    16,
			PrefetchConcurrency: 8,
		},
		Upstreams: map[string]UpstreamConfig{
			"hub": {
				Registry:         "https://registry-1.docker.io",
				MirrorPolicy:     "ordered",
				DefaultNamespace: "library",
				TagTTL:           10 * time.Minute,
				Blob: UpstreamBlobConfig{
					MirrorPolicy:              "ordered",
					TopN:                      3,
					MaxConcurrencyPerEndpoint: 0,
				},
				Probe: UpstreamProbeConfig{
					Enabled:  false,
					Interval: 30 * time.Second,
					Timeout:  3 * time.Second,
					Cooldown: 2 * time.Minute,
				},
				Auth: AuthConfig{
					Type: "anonymous",
				},
				HTTP: HTTPConfig{
					Timeout: 0,
					Retry: HTTPRetryConfig{
						Enabled:    true,
						MaxRetries: 2,
						WaitMin:    100 * time.Millisecond,
						WaitMax:    1 * time.Second,
					},
				},
			},
		},
	}
}

func DefaultConfig() Config {
	return defaultConfig()
}
