# Design

## Positioning

RegiMux is a read-only developer dependency cache gateway. Container registry, Go, npm, PyPI, and Maven are first-class ecosystems. Configuration is split by ecosystem through `container`, `go`, `npm`, `pypi`, and `maven` blocks, and each ecosystem owns its endpoint service and runtime implementation under `internal/ecosystems/*`.

The container ecosystem exposes a Registry-compatible pull API and routes requests to configured upstream registries by container alias. Go, npm, PyPI, and Maven expose read-through proxy cache APIs under their own path prefixes and route requests to upstreams configured in their matching ecosystem blocks.

RegiMux is not a push registry. Upload, manifest write, and delete APIs are intentionally out of scope.

## Architecture Overview

```mermaid
flowchart LR
  client["Clients<br/>Docker / Go / npm / PyPI / Maven"] --> api["Fiber HTTP Server"]
  api --> auth["Auth Middleware"]
  auth --> routes["Ecosystem Routes"]
  routes --> container["container runtime<br/>Registry V2 / mirrors / prefetch"]
  routes --> golang["Go runtime<br/>module proxy cache"]
  routes --> npm["npm runtime<br/>registry cache"]
  routes --> pypi["PyPI runtime<br/>simple index / file cache"]
  routes --> maven["Maven runtime<br/>repository layout cache"]

  container --> upstreams["Upstream endpoints / mirrors"]
  golang --> upstreams
  npm --> upstreams
  pypi --> upstreams
  maven --> upstreams

  container --> meta["Metadata store<br/>SQLite / MySQL / PostgreSQL"]
  golang --> meta
  npm --> meta
  pypi --> meta
  maven --> meta

  container --> objects["Object store<br/>local / memory / S3 / SFTP"]
  golang --> objects
  npm --> objects
  pypi --> objects
  maven --> objects

  scheduler["Scheduler + Worker Pool"] --> container
  scheduler --> golang
  scheduler --> npm
  scheduler --> pypi
  scheduler --> maven
  admin["Admin UI"] --> scheduler
  admin --> meta
```

## OCI Request Model

Image names use the first repository path segment as the container alias:

```text
localhost:5000/{containerAlias}/library/alpine:latest
localhost:5000/{containerAlias}/org/app:v1.2.3
```

Registry API examples:

```text
GET /v2/{containerAlias}/library/alpine/manifests/latest
GET /v2/{containerAlias}/library/alpine/blobs/sha256:...
GET /v2/{containerAlias}/library/alpine/tags/list
GET /v2/{containerAlias}/library/alpine/referrers/sha256:...
```

The container alias is resolved from the `container` block. The rest of the path is passed to the selected upstream registry.

## Go Module Proxy Request Model

Go upstreams are configured under the `go` ecosystem block:

```hcl
go {
  default {
    registry = "https://proxy.golang.org"
  }
}
```

Clients use:

```bash
GOPROXY=http://localhost:5000/go/{goAlias},direct
```

Go proxy API examples:

```text
GET /go/{goAlias}/github.com/pkg/errors/@v/list
GET /go/{goAlias}/github.com/pkg/errors/@v/v0.9.1.info
GET /go/{goAlias}/github.com/pkg/errors/@v/v0.9.1.mod
GET /go/{goAlias}/github.com/pkg/errors/@v/v0.9.1.zip
GET /go/{goAlias}/github.com/pkg/errors/@latest
```

The Go alias is resolved only within the `go` block. It does not share a namespace with container, npm, PyPI, or Maven aliases.

`@latest` and `@v/list` use a short TTL. Versioned `.info`, `.mod`, and `.zip` responses are stored in object storage by content sha256, with metadata mapping module/reference to digest. The current implementation does not proxy `sum.golang.org` and does not perform VCS direct fetching.

## Read-through Cache Flow

```mermaid
sequenceDiagram
  participant C as Client
  participant A as RegiMux API
  participant R as Ecosystem Runtime
  participant M as Metadata Store
  participant O as Object Store
  participant U as Upstream Endpoint

  C->>A: GET artifact / manifest / blob
  A->>R: resolve ecosystem alias and reference
  R->>M: lookup repository metadata
  R->>O: lookup cached object by digest/key
  alt cache hit
    O-->>R: cached bytes
    R-->>A: validated cached response
  else cache miss or TTL refresh
    R->>U: fetch from selected upstream
    U-->>R: upstream response
    R->>O: store content-addressed bytes
    R->>M: upsert repository, tag, blob, pull, health data
    R-->>A: upstream response
  end
  A-->>C: compatible protocol response
```

## Other Ecosystem Prefixes

npm, PyPI, and Maven are first-class read-through proxy ecosystems with independent alias namespaces under their own path prefixes:

```text
GET /npm/{npmAlias}/...
GET /pypi/{pypiAlias}/...
GET /maven/{mavenAlias}/...
```

## Ecosystem Runtime Abstraction

Registry, mirror, probe, and prefetch behavior is exposed through ecosystem runtimes instead of being hard-coded into the scheduler. Each runtime owns the protocol details for one ecosystem, advertises the capabilities and jobs it supports, and is registered through `dix`.

Ecosystem implementations live under `internal/ecosystems/*`:

- `internal/ecosystems/container`
- `internal/ecosystems/golang`
- `internal/ecosystems/npm`
- `internal/ecosystems/pypi`
- `internal/ecosystems/maven`

The scheduler consumes the runtime set from `dix` and registers background work from `ecosystem.JobProvider` / `ecosystem.JobSpec`:

- `probe`: discover endpoint health and latency for aliases that configure mirror probing.
- `prefetch`: warm likely future artifacts through the same cache path used by client requests.
- `manifest_refresh`: refresh manifest metadata without forcing blob downloads where the ecosystem supports that distinction.
- `endpoint_health_flush`: persist buffered endpoint health state for runtimes that maintain a hot health layer.

Current capability coverage is intentionally uneven. The container runtime supports predictive `prefetch` because OCI pulls already depend on mirror scoring and manifest/blob warming. Go, npm, PyPI, and Maven support the shared endpoint `probe` capability and recent-pull `prefetch` rewarming through the same runtime registration boundary; ecosystem-specific version prediction can be added without changing scheduler wiring.

Manual sync is also standardized in the same abstraction:

- Admin submits `prefetch.SyncOptions` with `(ecosystem, alias, repo, reference)`.
- Scheduler selects the ecosystem runtime by `runtime.Name()` and submits a one-time background job via `SubmitSync`.
- A runtime that supports manual sync exposes `CreateSyncJob`, `RunSyncJob`, `SyncJob`, and `MarkSyncJobFailed`.
- Manual sync execution is isolated per ecosystem runtime but observed through shared scheduler metrics and admin job polling.
- Job lifecycle is in-memory today (in a concurrent map); results are returned from the scheduler endpoint and UI polling.

Because this is the same runtime boundary, adding a new ecosystem requires only:

1. implementing `ecosystem.Runtime` plus relevant capability interfaces (`Probe`, `Prefetch`, manual-sync, `JobProvider`).
2. registering it in `dix` with a stable key.
3. no changes to scheduler orchestration code.

```mermaid
flowchart TD
  dix["dix runtime registration"] --> runtimes["[]ecosystem.Runtime"]
  runtimes --> scheduler["Scheduler"]
  runtimes --> routes["HTTP route adapters"]

  scheduler --> jobs["runtime-declared jobs"]
  jobs --> probe["probe"]
  jobs --> prefetch["prefetch"]
  jobs --> refresh["manifest_refresh"]
  jobs --> flush["endpoint_health_flush"]

  admin["Admin manual sync"] --> syncopts["prefetch.SyncOptions"]
  syncopts --> scheduler
  scheduler --> selected["selected runtime by Name()"]
  selected --> onetime["gocron OneTimeJob"]
  onetime --> report["prefetch.SyncReport"]
  report --> admin
```

## Main Components

```mermaid
flowchart TB
  subgraph entry["Entry Layer"]
    http["Fiber HTTP server"]
    registry["Registry API handlers"]
    proxy["Go / npm / PyPI / Maven proxy handlers"]
    authmw["Auth middleware"]
    adminui["Admin UI"]
  end

  subgraph runtimes["Ecosystem Runtimes"]
    cr["container: Registry V2, mirrors, probe, prefetch"]
    gr["Go: module proxy cache, endpoint probe"]
    nr["npm: registry cache, endpoint probe"]
    pr["PyPI: simple index and file cache, endpoint probe"]
    mr["Maven: repository layout cache, endpoint probe"]
  end

  subgraph storage["Storage Layer"]
    metadata["Metadata store: SQLite / MySQL / PostgreSQL"]
    objectstore["Object store: local / memory / S3-compatible / SFTP"]
  end

  http --> authmw
  authmw --> registry
  authmw --> proxy
  http --> adminui
  registry --> cr
  proxy --> gr
  proxy --> nr
  proxy --> pr
  proxy --> mr
  cr --> metadata
  gr --> metadata
  nr --> metadata
  pr --> metadata
  mr --> metadata
  cr --> objectstore
  gr --> objectstore
  nr --> objectstore
  pr --> objectstore
  mr --> objectstore
```

Background services run through the scheduler and worker pool:

- cache cleanup and capacity control
- runtime-declared mirror probing
- runtime-declared predictive prefetch and manifest refresh
- distributed locks when Redis or Valkey is configured
- Redis/Valkey endpoint health hot state when a remote cache backend is configured

## Metadata Model

The metadata layer is SQL-backed and implemented with `dbx` repositories. Supported drivers:

- SQLite
- MySQL
- PostgreSQL

Metadata is organized around repository-style interfaces:

- catalog metadata for upstreams and repositories
- manifests and tags
- blobs and repository-to-blob links
- pull records
- endpoint health
- prefetch runs, outcomes, and controls
- aggregate read model for admin and stats

Endpoint health is durable in SQL. When Redis or Valkey is configured as the cache backend, probe updates are also written to a shared hot state layer so replicas can avoid cold-starting endpoint scores and can share low-latency mirror ranking quickly.

The SQL implementation is named `SQLStore`. SQLite-specific path, DSN, and pragma logic is isolated under the SQLite driver helper.

## Object Model

Blob objects are stored separately from metadata. The object store can be:

- local filesystem
- memory
- S3-compatible storage
- SFTP

Object keys are content-addressed where possible. Metadata remains the source of truth for whether an object is available for a repository.

## Cache Behavior

Client-facing requests use cache-first semantics: if RegiMux can open a local cached object, it returns that object immediately. TTL expiry does not block the client request on upstream validation. TTL only decides whether a background refresh should be triggered. A client request reaches upstream synchronously only when the matching local cache object is absent.

Background refresh paths include scheduled jobs, async refreshes triggered by stale cache hits, and Admin manual refresh. These paths may bypass the local-first rule, contact upstream directly, and update local metadata and object cache when upstream content changed.

Manifests are cached with an `Accept`-aware key because different clients may ask for different manifest media types for the same tag.

Blob caching is content-addressed by digest. Before returning a cached blob, RegiMux still checks that the requested repository is allowed to reference the digest.

Tags and referrers are cached with TTLs and upstream revalidation.

## Mirror Scheduling

One container alias may have multiple mirrors. The container runtime advertises the `probe` capability when probing is enabled for an alias. Blob fetches can use latency-aware selection:

- probes update endpoint latency and health
- successful endpoints are preferred
- failing endpoints enter cooldown windows
- content mismatch can temporarily downgrade a mirror

Client-side layer concurrency already exists in Docker/containerd, so RegiMux focuses on selecting better mirrors and avoiding slow or unhealthy endpoints.

## Prefetch

Container prefetch predicts likely next tags based on pull history, then warms manifests and blobs through the normal cache path. Dependency ecosystem prefetch currently rewinds recent pull history and refreshes the exact Go/npm/PyPI/Maven artifact through that ecosystem's proxy cache path. The scheduler invokes both through the runtime `prefetch` capability, so ecosystem-specific version prediction can be added behind the same job shape. Runs and outcomes are stored in metadata and shown in Admin UI.

Manual sync and scheduler prefetch share the same job abstraction:

- Prefetch jobs are periodic and periodicity is configured by `scheduler.prefetch`.
- Manual sync jobs are one-time and triggered via `/admin/sync` (form submit) and submitted as gocron `OneTimeJob`.
- Both produce `prefetch.SyncReport`-style outcomes and can be observed by admin endpoints and shared metrics.

Prefetch supports:

- byte budget
- task budget
- repository limit
- failure backoff
- retry window
- admin cancel/retry controls

## Authentication

When enabled, RegiMux supports Docker Registry authentication flow and `docker login`. Users are configured locally. Each user can be scoped to repository patterns such as:

```text
{containerAlias}/*
{containerAlias}/my-org/*
```

Admin UI reuses the same configured users and is protected with HTTP Basic when auth is enabled.

## Dependency Injection

The application is assembled with `dix`.

Important lifecycle decisions:

- logger, config, auth, scheduler, worker, admin, and store are shared modules; container-owned cache, upstream, registry tooling, suggestion, and Docker daemon integration live under the container ecosystem module set
- ecosystem runtime implementations are registered with `dix`; the scheduler consumes the registered runtime set rather than importing per-ecosystem handlers
- metadata mapper is a DI singleton
- `*dbx.DB` is managed by DI lifecycle and closed on stop
- SQL repositories are composed into a `meta.Store` facade while narrower repository interfaces are exposed for future consumers

## Non-goals

- no push/write Registry support
- no blob upload API
- no manifest PUT API
- no delete API
