# RegiMux Roadmap

RegiMux is expanding from a read-only OCI / Docker Registry V2 proxy mirror into a developer dependency cache gateway. The near-term scope stays read-only and cache-oriented: OCI first, then Go, Maven, PyPI, and npm last.

## Near Term

### Developer dependency cache gateway

- Keep the existing OCI / Docker Registry V2 compatible `/v2/{alias}/...` API as the stable registry path.
- Add upstream ecosystem types: `oci`, `go`, `maven`, `pypi`, and `npm`. Go is implemented; Maven, PyPI, and npm are reserved config values.
- Add a Go module proxy read-through cache at `/go/{alias}/{module}/@v/...` and `/go/{alias}/{module}/@latest`. Done.
- Use `golang` as the default Go upstream alias backed by `https://proxy.golang.org`. Clients can set `GOPROXY=http://localhost:5000/go/golang`. Done.
- Store Go proxy responses in the object store by content sha256, with metadata mapping request paths to object digests. Done.
- Add Maven next as a read-through repository-layout cache for release artifacts, `maven-metadata.xml`, and checksum files.
- Add PyPI next with PEP 503 simple index caching, normalized package names, and file link rewriting.
- Add npm last, covering packuments, dist-tags, scoped packages, tarball URL rewriting, and integrity metadata.

### S3-compatible object storage

- Add an `object` store driver for S3-compatible services such as AWS S3, MinIO, R2, and OSS-compatible deployments. Done.
- Keep the existing local filesystem object store as the default for single-node setups. Done.
- Support configurable bucket, endpoint, region, credentials, path style, and object key prefix. Done.
- Preserve digest verification on writes and reads. Done.
- Add integration examples for MinIO in Docker Compose.

### SFTP object storage

- Add an `object` store driver backed by `github.com/spf13/afero/sftpfs` for shared SFTP storage. Done.
- Reuse `afero.NewBasePathFs` so `store.object.path` is the remote object root. Done.
- Require host key verification with either `known_hosts_path` or a pinned `host_key`. Done.
- Add integration examples with an SFTP server container.

### Cache cleanup and capacity control

- Extend cleanup jobs with metadata-backed object-cache capacity watermarks. Done.
- Delete orphan blob metadata when the referenced object is already missing. Done.
- Support dry-run cleanup reports in logs and admin UI.
- Prefer last-access based eviction for blobs and repo-to-blob links. Done for blobs.

### Registry client compatibility tests

- Add end-to-end tests with Docker CLI, nerdctl/containerd, and ORAS.
- Cover Docker login, manifest HEAD/GET, blob range reads, multi-arch images, tags pagination, and referrers.
- Keep protocol-level unit tests for edge cases, but validate behavior with real clients before releases.

### Admin operations

- Add manual cleanup actions for repository, tag, digest, and orphan objects.
- Add mirror re-probe controls.
- Add recent error views for upstream, cache, scheduler, and metadata operations.
- Add background job history for cleanup and prefetch runs.

## Mid Term

### Prefetch policy controls

- Add per-run byte budgets, task budgets, and repository limits.
- Add failure backoff and retry windows for predicted images.
- Persist prefetch run history and per-candidate outcomes.
- Expose cancel/retry controls in admin.

### Mirror scheduling improvements

- Persist endpoint health snapshots so restarts do not cold-start all mirror scores.
- Track success rate by endpoint and repository.
- Add circuit breaker windows for repeatedly failing mirrors.
- Add jitter to background probes to avoid synchronized bursts.
- Detect inconsistent mirror content and temporarily downgrade affected endpoints.

### Metadata model expansion

- Promote upstream and repository metadata to first-class tables when admin/query features need them.
- Keep config as the source of truth for upstream definitions unless runtime metadata is explicitly required.
- Add repository-level aggregate stats for pulls, bytes, blob links, and last activity.

## Later

### Auth and policy

- Split registry pull permissions from admin permissions.
- Add password hash generation tooling.
- Add token revocation or short-lived token rotation support.
- Consider external identity providers such as OIDC or LDAP after the local config model is stable.

### Observability

- Provide a Grafana dashboard for Prometheus metrics.
- Add slow upstream request and slow DB operation summaries.
- Add trace support if observabilityx tracing is adopted across HTTP, cache, upstream, and DB layers.
- Add a diagnostics export endpoint or admin action.

### Deployment

- Add Helm chart examples.
- Add production deployment notes for Redis/Valkey, PostgreSQL/MySQL, and S3/MinIO.
- Add CI coverage for supported metadata drivers and object store drivers.
- Keep release artifacts aligned across archive, deb, rpm, Windows exe, and Docker images.

## Non-goals For Now

- No push/write registry support is planned; RegiMux remains read-only.
