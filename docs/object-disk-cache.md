# S3 object disk cache

RegiMux can keep a bounded, per-instance disk L1 in front of an S3-compatible object store. It is disabled by default. It is not used with the local object driver, whose objects are already stored on disk.

## Configuration

Add `disk_cache` inside `store.object` and enable it only when `store.object.driver = "s3"`:

```hcl
store {
  object {
    driver = "s3"
    path = ""

    s3 {
      bucket = "regimux-objects"
      prefix = "cache"
      region = "us-east-1"
      endpoint = "http://minio:9000"
      force_path_style = true
    }

    disk_cache {
      enabled = true
      directory = "./data/object-cache"
      max_bytes = 10737418240
      max_object_bytes = 1073741824
      max_entries = 10000
      max_concurrent_fills = 4
      ttl = "24h"
    }
  }
}
```

The defaults are disabled, directory `./data/object-cache`, 10 GiB total, 1 GiB per object, 10,000 entries, four concurrent fills, and a 24-hour TTL. When enabled, all limits must be positive, the per-object limit cannot exceed the total limit, and the directory cannot be empty or `.`.

The equivalent environment variables use the `REGIMUX_STORE__OBJECT__DISK_CACHE__` prefix, for example `REGIMUX_STORE__OBJECT__DISK_CACHE__ENABLED=true` and `REGIMUX_STORE__OBJECT__DISK_CACHE__MAX_BYTES=10737418240`.

## Deployment

Give each RegiMux instance its own cache directory. Do not share the directory between replicas. In the project image, `/var/lib/regimux` is the existing writable data directory owned by the runtime user; for example, configure `/var/lib/regimux/object-cache` and mount a dedicated volume at `/var/lib/regimux`. Give each replica its own volume. This preserves the image's directory ownership without hard-coding a UID, which can differ between image variants.

For Kubernetes, a dedicated `emptyDir` can be mounted at the configured cache directory or at `/var/lib/regimux`. Ensure the mounted directory is writable by the container runtime user with the pod's `fsGroup` or an appropriate volume owner/permission setup. A fresh `emptyDir` starts cold again after pod replacement. Keep the image's default runtime user.

The cache is a performance layer. S3 remains authoritative for `Stat`, `Exists`, `List`, and `Walk`. A warm Range read can return `206` from the local cached object; a cold Range read goes directly to S3 and does not publish a full-object L1 entry. `HEAD` and `Stat` continue to use S3. An object `Delete` removes the L1 copy as well as deleting the canonical S3 object. Capacity eviction only removes local entries and never deletes S3 objects, so it is not a substitute for the object cleanup job.

Writes do not eagerly populate the L1. The first complete read that fills the local cache remains an S3 read; a subsequent complete read can be served from the local disk cache.

Concurrent complete reads of the same eligible digest share one S3 read. Each request has an independent reader over the temporary disk file and can stream as bytes arrive. Canceling one request leaves the other readers running; when the last reader closes, an unfinished fill is canceled and cleaned up. Objects become eligible for publication after a complete read and successful size and digest validation.

The concurrent-fill limit applies to different cold objects. Reads that cannot be admitted because of size or capacity limits continue directly from S3. The implementation reuses Hashicorp LRU, gofrs/flock, and djherbis/stream, with filesystem access scoped through Go's `os.Root`.

Expiration is checked when reading an entry and restoring the index. Hits update recency in memory; after restart, expiration uses the last persisted timestamp, so an older entry may be refreshed from S3 conservatively. If temporary-file cleanup fails, further fills stop until restart while canonical reads continue.

## Metrics

The cache exposes bounded outcome counters without object keys:

- `regimux_service_object_disk_cache_accesses_total{operation,result}` counts cache accesses such as hits, misses, fills, evictions, and errors.
- `regimux_service_object_disk_cache_bytes` and `regimux_service_object_disk_cache_reserved_bytes` report current and reserved local capacity.
- `regimux_service_object_disk_cache_entries` reports complete local entries.
- `regimux_service_object_disk_cache_fills` reports active fills.

Estimate the GET hit ratio from `get,hit` divided by the sum of `get,hit` and `get,miss` over the same time window. Track the byte, entry, reservation, and active-fill gauges to see local capacity use and fill pressure.
