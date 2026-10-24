# RegiMux

RegiMux is a read-only dependency proxy for development and CI environments. Package managers and container runtimes point at RegiMux instead of talking to every public upstream directly; RegiMux routes requests to the configured upstreams, caches immutable artifacts in object storage, tracks metadata, and runs background probe, prefetch, refresh, and cleanup work.

Container registry, Go modules, npm, PyPI, Maven, and dist binary distribution mirrors are first-class dependency ecosystems. Each ecosystem keeps its own protocol adapter, endpoint service, runtime capabilities, and configuration namespace, while shared storage, scheduling, auth, and Admin UI code stay common.

## Documentation

- English documentation: [GitHub Wiki](https://github.com/lyonbrown4d/regimux/wiki/en-README)
- 中文文档：[GitHub Wiki](https://github.com/lyonbrown4d/regimux/wiki/zh-README)

## Upstream and cache failure handling

For anonymous public container mirrors, set `mirror_forbidden_policy = "public"` on the upstream to allow fallback when a mirror explicitly reports an allowlist restriction. The default is `"terminal"`. Anonymous Bearer challenges are supported; configured credentials and ordinary permission denials remain terminal. Coverage cooldown applies to the affected endpoint and repository.

OCI operations backed by ORAS validate bearer token realms and token-service redirects. Private token services on the registry hostname remain supported; a different private hostname is rejected. Expose a trusted private authentication service through the registry hostname when needed, preserving HTTPS for HTTPS registries.

Maven and PyPI accept `http { attempt_timeout = "10s" }` for each upstream attempt. Artifact transfers in all six ecosystems and cached-object reads share a separate 10-second default body inactivity budget starting at the first body read, so a healthy large download can continue beyond the header budget and an interrupted download can resume within package-manager deadlines. Explicit `http.timeout` and caller deadlines still apply.

Maven cache opens have a 1-second budget and cache writes a 30-second budget. PyPI cache reads and opens have a 2-second budget; package writes, prepared index writes, and pull recording use separate 30-second write budgets and still honor caller cancellation. Healthy content can be served when caching fails; Maven validates artifacts before taking that fallback. PyPI streams package responses, publishes only complete, validated full responses, and bounds concurrent temporary spools to eight per service.

After a pull count commits, SQL catalog updates have a separate 30-second completion budget, including one retry, even if the caller cancels. Retrying recalculates repository and upstream totals from stored records without counting the pull again.

Shared artifact metadata reads schedule blob access timestamps through a separate non-blocking worker pool with at most four active updates. Updates coalesce by digest and retain a 500-millisecond budget after the read completes. Saturation can skip these best-effort updates; scheduled and API package cleanup also use recorded pull activity to retain active package groups.

PyPI releases a response's temporary spool and cache-fill ownership after 30 seconds without a consumer read. This lease pauses during internal pull recording before response handoff and during an active read, including continuation attempts, so it does not compete with the per-attempt network timeout.

PyPI supports single byte-range requests, `If-Range`, and `206`/`416` responses. Cache hits use object-storage range reads, and partial responses never replace complete cached files. Wheel fallback discovers the exact filename and SHA-256 through configured simple indexes, including mirrors with different download paths. Old download URLs can recover their identity from a matching cached or fetched index link; without that evidence they retain the original upstream failure. Wheel metadata files keep a separate content identity from the wheel itself.

All six ecosystems support single byte-range downloads, `If-Range`, and `206`/`416` response headers. HEAD ignores Range. Go, npm, and Maven prepare the complete artifact before selecting a cold range; cached responses use a seekable stream or bounded discard when a native range opener is unavailable. Range selection preserves complete cache entries.

Interrupted artifact reads can reopen at the exact byte offset up to three times. HTTP continuation requires a known length and a strong ETag or trusted SHA-256; partial responses require a strong ETag because a whole-file hash cannot verify an isolated range. The proxy verifies the returned range, total length, validator, and encoding before appending bytes. Object-cache reads verify the immutable object identity and requested length on every attempt. Each attempt owns its inactivity timer, and closing the transfer or canceling its caller stops recovery. The shared implementation reuses the existing HTTP clients, `go-retry`, and `go-digest` dependencies.

## Transfer memory

OCI limits simultaneous metadata preparation to four responses per endpoint. After the response is spooled and its admission slot released, shared catalog writes run once with a separate 90-second commit budget and can finish after a client disconnects. Per-request pull records still honor caller cancellation.

Optional OCI child prewarming has four separate admission slots per endpoint. A slot covers descriptor parsing, catalog persistence and worker consumption. When all slots are occupied, requests skip prewarming. Cancellation before worker handoff and failed submission release the plan and its decoded descriptors; an accepted worker releases them when it finishes.

Concurrent catalog refreshes reuse the existing `go-singleflightx` dependency. Repository updates for the same upstream join a batch that locks the upstream row, refreshes the selected repositories, and aggregates the upstream once in one transaction. A shared blob digest submits its linked repositories together. Each caller waits for a batch covering its committed mutation; later writes trigger another batch. Coordination state is removed after active callers and refresh callbacks finish, and failed refreshes remain retryable even when the blob size is unchanged.

Artifact bodies stream through object storage and temporary files. S3 uploads use a fixed 32 KiB probe for unknown-length readers and stream multipart sections from seekable sources; existing cache spool files are reused. Content digests are verified before publishing objects. `store.object.s3.max_concurrent_uploads` defaults to 16 across objects, while `upload_concurrency` still controls parts within each object. Admission waits honor cancellation before consuming the input. Upload activity, wait duration, and completion outcomes are exposed through the `object_upload_*` service metrics.

S3 reads reuse the existing AWS SDK HTTP client with up to 1,024 idle connections per host and 2,048 idle connections overall. The SDK's active connection limit and timeouts remain in effect. Complete object downloads take their size, content type, and ETag from GET; range reads still resolve against HEAD before opening the selected bytes.

S3 deployments can optionally enable a bounded per-instance disk L1 for object bodies. It is disabled by default and requires a private cache directory for each RegiMux instance. See [S3 object disk cache](docs/object-disk-cache.md) for configuration and deployment guidance.

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
