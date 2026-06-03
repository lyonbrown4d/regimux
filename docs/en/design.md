# Design

## Positioning

RegiMux is a read-only developer dependency cache gateway. The stable handlers today are an OCI / Docker Registry V2 proxy mirror and a Go module proxy read-through cache. Configuration is split by ecosystem through `container`, `go`, `npm`, `pypi`, and `maven` blocks.

The OCI side exposes a Registry-compatible pull API while routing requests to configured container registries by container alias. The Go side exposes the Go module proxy protocol under `/go/{goAlias}/...` and routes requests to upstreams configured under the `go` block.

RegiMux is not a push registry. Upload, manifest write, and delete APIs are intentionally out of scope.

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

## Other Ecosystem Prefixes

npm, PyPI, and Maven use independent alias namespaces under their own path prefixes:

```text
GET /npm/{npmAlias}/...
GET /pypi/{pypiAlias}/...
GET /maven/{mavenAlias}/...
```

## Ecosystem Runtime Abstraction

Registry, mirror, probe, and prefetch behavior is exposed through ecosystem runtimes instead of being hard-coded into the scheduler. Each runtime owns the protocol details for one ecosystem and advertises the capabilities it supports.

The scheduler consumes the runtime set from `dix` and registers background work from capabilities:

- `probe`: discover endpoint health and latency for aliases that configure mirror probing.
- `prefetch`: warm likely future artifacts through the same cache path used by client requests.

Current capability coverage is intentionally uneven. The container runtime supports predictive `prefetch` because OCI pulls already depend on mirror scoring and manifest/blob warming. Go, npm, PyPI, and Maven support the shared endpoint `probe` capability and recent-pull `prefetch` rewarming through the same runtime registration boundary; ecosystem-specific version prediction can be added without changing scheduler wiring.

## Main Components

```text
Client
  |
  v
Fiber HTTP server
  |
  +-- Registry API handlers
  +-- Go proxy API handlers
  +-- npm / PyPI / Maven proxy handlers
  +-- Auth middleware
  +-- Admin UI
  |
  v
Ecosystem runtimes
  |
  +-- container runtime: Registry V2, mirrors, probe, prefetch
  +-- Go runtime: module proxy cache, endpoint probe
  +-- npm runtime: registry cache, endpoint probe
  +-- PyPI runtime: simple index and file cache, endpoint probe
  +-- Maven runtime: repository layout cache, endpoint probe
  |
  v
Storage
  |
  +-- Metadata store: SQLite / MySQL / PostgreSQL
  +-- Object store: local / memory / S3-compatible / SFTP
```

Background services run through the scheduler and worker pool:

- cache cleanup and capacity control
- capability-based mirror probing
- capability-based predictive prefetch
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

- logger, config, auth, ecosystem runtimes, cache, upstream, scheduler, worker, admin, and store are separate modules
- ecosystem runtime implementations are registered with `dix`; the scheduler consumes the registered runtime set rather than importing per-ecosystem handlers
- metadata mapper is a DI singleton
- `*dbx.DB` is managed by DI lifecycle and closed on stop
- SQL repositories are composed into a `meta.Store` facade while narrower repository interfaces are exposed for future consumers

## Non-goals

- no push/write Registry support
- no blob upload API
- no manifest PUT API
- no delete API
