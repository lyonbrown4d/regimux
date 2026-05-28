# Scheduler

RegiMux uses `gocron` for background jobs and a worker pool for bounded asynchronous work.

## Jobs

Current jobs:

- cache cleanup and object capacity control
- upstream mirror probing
- predictive prefetch

When Redis or Valkey is configured, scheduler jobs can use distributed locks to avoid duplicate work across replicas.

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

Each upstream can probe mirrors and persist endpoint health:

```hcl
upstreams {
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

Blob fetches prefer healthy low-latency endpoints. Failing endpoints enter cooldown windows, and content mismatches can downgrade an endpoint.

## Predictive Prefetch

Prefetch predicts likely next tags from pull history, then warms manifests and referenced blobs through the same cache path as client pulls.

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

Runs and outcomes are stored in metadata and can be viewed from Admin UI.

## Worker Pool

```hcl
worker {
  probe_concurrency = 16
  prefetch_concurrency = 8
}
```

Set these values based on upstream rate limits, object store bandwidth, and available CPU/network capacity.

