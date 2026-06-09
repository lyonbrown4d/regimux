# Scheduler

RegiMux uses `gocron` for background jobs and a worker pool for bounded asynchronous work.

## Jobs

Current jobs:

- cache cleanup and object capacity control
- runtime-declared `probe` jobs
- runtime-declared `prefetch` jobs
- runtime-declared `manifest_refresh` jobs (manifest-only warmup where applicable)

When Redis or Valkey is configured, scheduler jobs can use distributed locks to avoid duplicate work across replicas. Probe jobs also publish endpoint health into Redis/Valkey hot state, while SQL metadata remains the durable source of truth.

The scheduler does not own ecosystem-specific fetch logic. Ecosystem modules register runtimes through `dix`, each runtime advertises `ecosystem.JobSpec` values through `JobProvider`, and the scheduler only translates those specs into `gocron` jobs. Container, Go, npm, PyPI, and Maven can add or remove ecosystem-specific background work without changing scheduler orchestration.

## Cleanup

Cleanup removes cached blob objects that have not been accessed for `scheduler.cleanup.unused_for`.

```hcl
scheduler {
  cleanup {
    enabled = true
    interval = "1h"
    unused_for = "168h"
    max_deletes = 1000
    max_bytes = 10737418240
    target_bytes = 8589934592
  }
}
```

When `max_bytes` and `target_bytes` are set, RegiMux evicts least-recently-accessed unprotected blobs until the object cache reaches the target or the scan/delete limits are reached.

## Mirror Probing

Runtimes that implement `probe` can schedule mirror health checks and persist endpoint health. Container aliases use this for latency-aware blob mirror selection:

```hcl
container {
  hub {
    blob {
      mirror_policy = "latency"
      top_n = 3
      max_concurrent_attempts = 1
    }

    probe {
      enabled = true
      interval = "30s"
      timeout = "3s"
      cooldown = "2m"
      jitter = "5s"
    }
  }
}
```

Container blob fetches prefer healthy low-latency endpoints. Failing endpoints enter cooldown windows, and content mismatches can downgrade an endpoint.

Go, npm, PyPI, and Maven aliases can enable the same endpoint reachability probe:

```hcl
npm {
  default {
    registry = "https://registry.npmjs.org"
    mirrors = ["https://registry.npmmirror.com"]

    probe {
      enabled = true
      interval = "1m"
      timeout = "3s"
      cooldown = "2m"
      jitter = "10s"
    }
  }
}
```

Endpoint health is stored in SQL metadata and, when the cache backend is Redis or Valkey, mirrored into a hot state index made of endpoint Hash records plus alias-level Set and ZSet indexes. Dependency ecosystem probe records use scoped metadata aliases such as `npm/default`, so they do not collide with container aliases.

## Predictive Prefetch

Runtimes that implement `prefetch` can schedule cache warming. Container prefetch predicts likely next tags from pull history, then warms manifests and referenced blobs through the same cache path as client pulls. Go, npm, PyPI, and Maven currently implement recent-pull rewarming: once an artifact has been requested by a client, scheduled prefetch can refresh that exact artifact through the ecosystem proxy cache path.

```hcl
scheduler {
  prefetch {
    enabled = true
    interval = "30m"
    min_pull_count = 2
    max_candidates_per_repo = 3
    max_version_distance = 5
    max_bytes = 0
    max_tasks = 0
    max_repositories = 0
    failure_backoff = "1h"
    retry_window = "24h"
  }
}
```

Runs and outcomes are stored in metadata and can be viewed from Admin UI. Dependency ecosystem prefetch records use scoped aliases such as `go/default` or `npm/default`; version prediction for npm/PyPI/Maven/Go is intentionally left as a later ecosystem-specific layer.

## Manifest Refresh

Manifest refresh runs the same prefetch scheduling pipeline, but in manifest-only mode. It only fetches manifest metadata (including index child manifests) and does not download blob content. This is useful for keeping repository manifest metadata fresh across aliases and mirrors without bandwidth cost of full blob prefetch.

```hcl
scheduler {
  manifest_refresh {
    enabled = true
    interval = "30m"
    distributed = true

    ecosystems {
      container {
        interval = "10m"
      }

      go {
        enabled = false
      }
    }
  }
}
```

When `ecosystems` is omitted, one manifest refresh job covers every runtime with prefetch support. When `ecosystems` is present, each runtime gets its own effective schedule: unspecified fields inherit from the top-level `manifest_refresh` block, and `enabled = false` disables that ecosystem.

## Recent-Pull Refresh

Client-facing service APIs stay cache-first. When a request hits local cache, including stale metadata, the service publishes `artifact.pulled` instead of forcing upstream refresh inline. The scheduler stores a refresh intent in metadata and deduplicates by `(ecosystem, kind, alias, repository, reference, accept)` for `scheduler.refresh.window`.

```hcl
scheduler {
  refresh {
    enabled = true
    window = "10m"
    distributed = true
  }
}
```

The default window is 10 minutes. If the same artifact is pulled 100 times inside that window, only one due refresh intent is consumed. After the window expires and the intent has been consumed, a later pull can create the next refresh intent.

## Worker Pool

```hcl
worker {
  probe_concurrency = 16
  prefetch_concurrency = 8
}
```

Set these values based on upstream rate limits, object store bandwidth, and available CPU/network capacity.
