# Design

## Positioning

RegiMux is a read-only developer dependency cache gateway. The stable capabilities today are an OCI / Docker Registry V2 proxy mirror and a Go module proxy read-through cache. Maven, PyPI, and npm adapters are planned next.

The OCI side exposes a Registry-compatible pull API while routing requests to configured upstream registries by alias. The Go side exposes the Go module proxy protocol at the root path and routes requests to upstreams configured with `type = "go"`. The compatibility path `/go/{alias}/...` can still target one Go upstream explicitly.

RegiMux is not a push registry. Upload, manifest write, and delete APIs are intentionally out of scope.

## OCI Request Model

Image names use the first repository path segment as the upstream alias:

```text
localhost:5000/hub/library/alpine:latest
localhost:5000/ghcr/org/app:v1.2.3
```

Registry API examples:

```text
GET /v2/hub/library/alpine/manifests/latest
GET /v2/hub/library/alpine/blobs/sha256:...
GET /v2/hub/library/alpine/tags/list
GET /v2/hub/library/alpine/referrers/sha256:...
```

The alias is resolved from configuration. The rest of the path is passed to the selected upstream registry.

## Go Module Proxy Request Model

Go upstreams use `type = "go"`:

```hcl
upstreams {
  golang {
    type = "go"
    registry = "https://proxy.golang.org"
  }
}
```

Clients use:

```bash
GOPROXY=http://localhost:5000,direct
```

Go proxy API examples:

```text
GET /github.com/pkg/errors/@v/list
GET /github.com/pkg/errors/@v/v0.9.1.info
GET /github.com/pkg/errors/@v/v0.9.1.mod
GET /github.com/pkg/errors/@v/v0.9.1.zip
GET /github.com/pkg/errors/@latest
```

Root Go proxy requests try all configured Go upstreams in stable alias order, preferring the `golang` alias when present and falling back to later Go upstreams when a module is not available.

`@latest` and `@v/list` use a short TTL. Versioned `.info`, `.mod`, and `.zip` responses are stored in object storage by content sha256, with metadata mapping module/reference to digest. The current implementation does not proxy `sum.golang.org` and does not perform VCS direct fetching.

## Main Components

```text
Client
  |
  v
Fiber HTTP server
  |
  +-- Registry API handlers
  +-- Go proxy API handlers
  +-- Auth middleware
  +-- Admin UI
  |
  v
Cache services
  |
  +-- OCI manifest cache
  +-- OCI blob cache
  +-- OCI tags cache
  +-- OCI referrers cache
  +-- Go module proxy cache
  |
  v
Storage
  |
  +-- Metadata store: SQLite / MySQL / PostgreSQL
  +-- Object store: local / memory / S3-compatible / SFTP
```

Background services run through the scheduler and worker pool:

- cache cleanup and capacity control
- upstream mirror probing
- predictive prefetch
- distributed locks when Redis or Valkey is configured

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

One upstream alias may have multiple mirrors. Blob fetches can use latency-aware selection:

- probes update endpoint latency and health
- successful endpoints are preferred
- failing endpoints enter cooldown windows
- content mismatch can temporarily downgrade a mirror

Client-side layer concurrency already exists in Docker/containerd, so RegiMux focuses on selecting better mirrors and avoiding slow or unhealthy endpoints.

## Prefetch

Prefetch predicts likely next tags based on pull history, then warms manifests and blobs through the normal cache path. Runs and outcomes are stored in metadata and shown in Admin UI.

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
hub/*
ghcr/my-org/*
```

Admin UI reuses the same configured users and is protected with HTTP Basic when auth is enabled.

## Dependency Injection

The application is assembled with `dix`.

Important lifecycle decisions:

- logger, config, auth, cache, upstream, scheduler, worker, admin, and store are separate modules
- metadata mapper is a DI singleton
- `*dbx.DB` is managed by DI lifecycle and closed on stop
- SQL repositories are composed into a `meta.Store` facade while narrower repository interfaces are exposed for future consumers

## Non-goals

- no push/write Registry support
- no blob upload API
- no manifest PUT API
- no delete API
