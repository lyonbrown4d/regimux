# Admin UI

The Admin UI is embedded into the RegiMux binary. It uses Fiber template rendering, embedded templates, embedded i18n resources, Tailwind CSS from CDN, and htmx from CDN.

Open:

```text
http://localhost:5000/admin
```

## Security

When `auth.enabled = true`, Admin UI is protected with HTTP Basic using the same configured users as Registry auth.

## Language and Theme

Admin UI supports English and Chinese. Locale resources are embedded in the binary.

The UI follows the browser or operating system light/dark preference automatically.

## Views

Current views:

- dashboard
- upstream health
- pull and activity history
- cache status
- storage and large blobs
- scheduler jobs, prefetch runs, and prefetch outcomes
- manual refresh
- auth audit
- effective configuration

## Storage Metrics

Admin storage and cache counters are metadata-backed accounting, not a live scan of the object store path or bucket.

- `Committed Blob Bytes (metadata)` is the sum of committed `meta_blobs.size` rows. A blob only appears here after object storage has accepted the blob and metadata has been committed.
- Storage total is the current tracked accounting sum: committed blob metadata size plus manifest object bytes recorded as manifest metadata size.
- `Object Store Bytes (listed)` is a live CAS object listing from `store.object` when the active driver exposes object walking. It is useful for reconcile and dry-run checks against metadata-backed accounting, and it may be unavailable if the driver cannot list objects or the list operation fails.
- The configured KV cache backend (`cache.backend`, such as Redis or Valkey) is separate from `store.object`, where committed blob and manifest objects are stored.

## Manual Refresh

Manual refresh is ecosystem-aware. It bypasses the normal cache-first read path, checks the selected upstream, and updates the local cache when upstream content changed. In the upstream selector you can choose

- `container:<alias>` for OCI/container images
- `go:<alias>` for Go module proxy
- `npm:<alias>` for npm
- `pypi:<alias>` for PyPI
- `maven:<alias>` for Maven

For each ecosystem, fill the same `Repository` / `Reference` fields:

- `container`: repository format uses container image path, e.g. `library/node` with reference `20`.
- `go`: repository is the module path, e.g. `github.com/pkg/errors`, reference is version/tag like `v0.9.1`.
- `npm`: repository is the package name, reference is the version/tag, e.g. `react`, `18.2.0`.
- `pypi`: repository is package name, reference is version/tag.
- `maven`: repository is group/artifact path, reference is version/version segment.

Examples:

```text
container:hub / repository=library/node / reference=20
go:default / repository=github.com/pkg/errors / reference=v0.9.1
npm:default / repository=react / reference=18.2.0
pypi:default / repository=urllib3 / reference=2.2.0
maven:central / repository=com/fasterxml/jackson/core/jackson-databind / reference=2.16.1
```

The job is created asynchronously and can be viewed from the refresh result panel. It is useful when the normal background refresh has not caught up yet and an operator wants to force upstream freshness immediately.

### Flow

- Submit form: `POST /admin/sync` creates a refresh job (status `queued`) and schedules an immediate background job.
- Poll status: result panel uses `GET /admin/sync/jobs/{id}` and auto-refreshes every 2 seconds via htmx while status is `queued` or `running`.
- Completion states:
  - `queued`
  - `running`
  - `succeeded`
  - `failed`

The final result contains:

- `alias` / `repository` / `reference`
- manifest digest and media type
- warmed artifact counts:
  - layers
  - blobs
  - child manifests
- elapsed duration

### Input behavior

- `repository` can be provided as `repo:tag` or `repo@digest` to be auto-split into `Reference`.
- If `reference` is empty, RegiMux uses `latest` by default.
- For container ecosystem, input is parsed as a manifest path and default namespace is applied from `container` config (for example `library/*`).
- For non-container ecosystems, `repository`/`reference` are passed directly after alias validation.

### Error handling

The refresh page returns different status codes depending on failure source:

- `400` validation error (for example missing repository)
- `503` refresh service unavailable
- `502` when scheduling or refresh submission fails
- `404` when querying an unknown job ID

Manual refresh jobs are currently kept in scheduler memory and exposed through the job polling endpoint; they are not persisted as a standalone history table.
