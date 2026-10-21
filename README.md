# RegiMux

RegiMux is a read-only dependency proxy for development and CI environments. Package managers and container runtimes point at RegiMux instead of talking to every public upstream directly; RegiMux routes requests to the configured upstreams, caches immutable artifacts in object storage, tracks metadata, and runs background probe, prefetch, refresh, and cleanup work.

Container registry, Go modules, npm, PyPI, Maven, and dist binary distribution mirrors are first-class dependency ecosystems. Each ecosystem keeps its own protocol adapter, endpoint service, runtime capabilities, and configuration namespace, while shared storage, scheduling, auth, and Admin UI code stay common.

## Documentation

- English documentation: [GitHub Wiki](https://github.com/lyonbrown4d/regimux/wiki/en-README)
- 中文文档：[GitHub Wiki](https://github.com/lyonbrown4d/regimux/wiki/zh-README)

## Upstream and cache failure handling

For anonymous public container mirrors, set `mirror_forbidden_policy = "public"` on the upstream to allow fallback when a mirror explicitly reports an allowlist restriction. The default is `"terminal"`. Anonymous Bearer challenges are supported; configured credentials and ordinary permission denials remain terminal. Coverage cooldown applies to the affected endpoint and repository.

Maven and PyPI accept `http { attempt_timeout = "10s" }` for each upstream attempt. Maven artifact transfers and PyPI package transfers keep a separate 30-second body inactivity budget, so a healthy large download can continue beyond the header budget. Explicit `http.timeout` and caller deadlines still apply.

Maven cache opens have a 1-second budget and cache writes a 30-second budget. PyPI cache stages have a 2-second budget. Healthy content can be served when caching fails; Maven validates artifacts before taking that fallback. PyPI streams package responses, publishes only complete, validated full responses, and bounds concurrent temporary spools to eight per service.

PyPI supports single-byte ranges, `If-Range`, and `206`/`416` responses. Cache hits use object-storage range reads, and partial responses never replace complete cached files. Wheel fallback discovers the exact filename and SHA-256 through configured simple indexes, including mirrors with different download paths. Old download URLs can recover their identity from a matching cached or fetched index link; without that evidence they retain the original upstream failure. Wheel metadata files keep a separate content identity from the wheel itself.

## Transfer memory

Artifact bodies stream through object storage and temporary files. S3 uploads use a fixed 32 KiB probe for unknown-length readers and stream multipart sections from seekable sources; existing cache spool files are reused. Content digests are verified before publishing objects. `store.object.s3.max_concurrent_uploads` defaults to 16 across objects, while `upload_concurrency` still controls parts within each object. Admission waits honor cancellation before consuming the input. Upload activity, wait duration, and completion outcomes are exposed through the `object_upload_*` service metrics.

The memory cache defaults to 10,000 entries and 64 MiB of combined key/value bytes. Configure `cache.memory.max_items` and `max_bytes` to override these bounds; zero selects the defaults. Oversized entries bypass the cache. These limits exclude caller-owned copies and Go runtime overhead, so they are not a process RSS limit.

npm and PyPI admit two metadata transformations per service and spool their results before serving slow clients. Input and rewritten-output limits are 64 MiB for npm metadata and 32 MiB for PyPI indexes. PyPI checks its budget while rewriting links; npm checks cumulative URL growth and escaped JSON output size before encoding. These are body-size limits; decoded metadata and runtime overhead also consume memory. OCI HTTP metadata requests admit four cache-decode/fetch operations at a time and spool response bodies before releasing admission. Temporary files are removed after response completion or cancellation.

JSON metadata is checked before decoding into objects. npm allows up to 1,000,000 structural characters (`{}[],:` outside strings); OCI uses 100,000. Both limit nesting to 128 levels, cumulative raw string content to 32 MiB, and each non-string token to 1 KiB, and require valid UTF-8 JSON. These checks prevent small JSON inputs from allocating millions of decoded objects.

Merged Maven group metadata admits two operations per service, limits combined member input and merged output to 16 MiB, and encodes XML directly into a bounded temporary file. HEAD counts encoded bytes without retaining a response body. The optional OCI small-blob cache shares four slots for decoding or recording; busy requests use object storage directly, cache hits stream from temporary files, and oversized recordings discard their buffers immediately.

Maven XML parsing limits each member to 100,000 elements, 100,000 attributes, and 128 levels of nesting. Each XML token receives a 4 MiB input-read budget, with a small amount of decoder lookahead. This bounds allocation for a single large start tag before its attributes are inspected.

## Cache cleanup

The unified cleanup job asks each ecosystem to plan idle logical cache groups, then applies the selected metadata removals together and sweeps shared content-addressed objects only when no remaining metadata references them. This keeps related artifacts together—for example, a Maven release's JAR, POM, checksums, and signatures—and prevents one ecosystem's cleanup from removing content still referenced by another package. PyPI groups each file with its matching metadata and known sidecars, while its project index stays separate; cleanup does not infer release groups from opaque filenames. Configure the job once under `scheduler.cleanup`; it covers container images, Go modules, npm, PyPI, Maven, and distribution archives. By default it runs daily at 03:00 China Standard Time (`cron = "0 3 * * *"`, `timezone = "Asia/Shanghai"`) and retains groups used within the last seven days (`unused_for = "168h"`). A positive `interval` remains available as a backward-compatible schedule override. A zero `max_scan` or `max_bytes` leaves that limit unbounded; `max_scan` counts package groups, and `max_deletes` limits unique candidate objects while allowing one oversized group to finish rather than leaving part of that group behind.

Cleanup retention measures how long an artifact has gone unused. Ecosystem cache freshness settings still determine whether a cached version or index should be refreshed while it is in use; a short freshness TTL does not make an actively used artifact eligible for deletion. Start with `dry_run = true` to inspect the planned work, then disable dry-run after reviewing the cleanup logs. `max_scan`, `max_deletes`, `max_bytes`, and `target_bytes` can bound each pass or reclaim storage toward a capacity target.

Cleanup reads metadata in pages of 256 rows. Each complete snapshot shares `scheduler.cleanup.metadata_max_records` (default 100,000) and `metadata_max_bytes` (default 32 MiB of raw row data) across all cleanup relations. The same limits independently bound unique references and their raw string bytes while walking the OCI manifest graph. Exceeding a snapshot or graph budget aborts the pass before deletion; it does not progressively clean a partial inventory. Raise the configured budget when a larger repository requires a complete snapshot and reference graph. Reconciliation streams the object walk and caps retained repair details, while reporting the total repair count and whether details were truncated.

Scheduled prefetch also pages its pull inventory with a 100,000-record/32 MiB default budget and fails before dispatching tasks if the complete inventory cannot fit. Snapshot byte budgets account for retained raw rows; one database page, decoded indexes, and runtime overhead can add to peak memory.

Cleanup rechecks access and reference metadata immediately before deleting an object. As with other cache eviction, a write racing with that final check can require the artifact to be fetched and cached again.

The same cleanup job works with local and S3-compatible object storage. The shared sweep deletes only unreferenced object keys beneath the configured object-store prefix. Keep bucket lifecycle expiration disabled for the RegiMux cache prefix so metadata and objects remain coordinated; RegiMux cleanup is the retention mechanism.

For multi-instance deployments, enable `scheduler.cleanup.distributed = true` and the scheduler's distributed lock. When Redis or Valkey is configured, only one instance runs each cleanup pass; with a local scheduler backend the job still runs without a distributed lock.

You can start the same all-ecosystem cleanup pass from the Admin API:

```sh
curl -X POST -H 'Accept: application/json' http://localhost:8080/admin/scheduler/cleanup
```

The request returns `202 Accepted` with `{"status":"accepted","scope":"all"}` when scheduled. Use the configured admin Basic credentials and any CSRF cookie/token required by your setup. The existing Admin page form continues to return its HTML response.
