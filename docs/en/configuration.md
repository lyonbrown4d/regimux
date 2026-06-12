# Configuration

RegiMux uses typed HCL config loaded through `configx`. The release packages and container image include a minimal config at `/etc/regimux/regimux.hcl`.

Configuration is organized around dependency proxy namespaces. Each top-level ecosystem block defines one or more aliases; each alias becomes a client-facing proxy prefix and points at one upstream plus optional mirrors, auth, probe, and HTTP policy.

## Files

Repository examples:

- [configs/regimux.minimal.hcl](../../configs/regimux.minimal.hcl): smallest runnable config.
- [configs/regimux.hcl](../../configs/regimux.hcl): practical default config.
- [configs/regimux.full.hcl](../../configs/regimux.full.hcl): full reference config.

Minimal config:

```hcl
server {
  listen = ":5000"
}

container {
  hub {
    registry = "https://registry-1.docker.io"
  }
}

go {
  default {
    registry = "https://proxy.golang.org"
  }
}

npm {
  default {
    registry = "https://registry.npmjs.org"
  }
}

pypi {
  default {
    registry = "https://pypi.org"
  }
}

maven {
  central {
    registry = "https://repo.maven.apache.org/maven2"
  }
}
```

## Defaults

Defaults are defined as typed Go config, then validated after file, dotenv, environment, and command-line sources are merged.

Important defaults:

- `server.listen = ":5000"`
- `server.public_url = "http://localhost:5000"`
- `server.middleware.request_id.enabled = true`
- `server.middleware.request_logger.enabled = false`
- `server.middleware.healthcheck.enabled = true` with `/livez` and `/readyz`
- `server.middleware.etag.enabled = true`, scoped away from registry `/v2` traffic
- `server.middleware.compress.enabled = true`, scoped away from registry `/v2` traffic
- `server.middleware.security_headers.enabled = true`, scoped away from registry `/v2` traffic
- `server.middleware.security_headers.cross_origin_embedder_policy = "unsafe-none"` so the embedded admin UI can load CDN assets
- `server.middleware.rate_limit.enabled = false`
- `server.middleware.csrf.enabled = false`
- `server.middleware.pprof.enabled = false`
- `cache.backend = ""` (KV cache disabled by default; set `memory`, `redis`, or `valkey` explicitly when needed)
- `cache.blob.stream_and_cache = true`
- `cache.blob.small_cache.enabled = false`
- `cache.blob.small_cache.max_size_bytes = 4194304`
- `cache.blob.small_cache.ttl = "24h"`
- `store.meta.driver = "sqlite"`
- `store.meta.path = "data/regimux.db"`
- `store.object.driver = "local"`
- `store.object.path = "data/objects"`
- `scheduler.cleanup.enabled = true`
- `scheduler.prefetch.enabled = false`
- `scheduler.refresh.enabled = true`
- `scheduler.refresh.window = "10m"` deduplicates recent-pull refresh intents per artifact before scheduling upstream refresh
- `scheduler.refresh.distributed = true`
- `scheduler.manifest_refresh.enabled = false`
- `scheduler.manifest_refresh.ecosystems` can override manifest refresh per ecosystem (`container`, `go`, `npm`, `pypi`, `maven`)
- `docker.enabled = false`
- `docker.observe = true`
- `docker.prewarm.alias = "hub"`
- `docker.prewarm.timeout = "10m"`
- `container.hub.registry = "https://registry-1.docker.io"`
- `container.ghcr.registry = "https://ghcr.io"`
- `container.quay.registry = "https://quay.io"`
- `container.hub.http.http2.enabled = false`
- `go.default.registry = "https://proxy.golang.org"`
- `npm.default.registry = "https://registry.npmjs.org"`
- `pypi.default.registry = "https://pypi.org"`
- `maven.central.registry = "https://repo.maven.apache.org/maven2"`

Top-level ecosystem blocks define dependency proxy namespaces:

- `container`: OCI / Docker Registry V2 dependency proxy aliases, exposed through `/v2/{containerAlias}/...`.
- `go`: Go module dependency proxy aliases, exposed through `/go/{goAlias}/...`.
- `npm`: npm dependency proxy aliases, exposed through `/npm/{npmAlias}/...`.
- `pypi`: PyPI dependency proxy aliases, exposed through `/pypi/{pypiAlias}/...`.
- `maven`: Maven dependency proxy aliases, exposed through `/maven/{mavenAlias}/...`.

These blocks are also the input to the ecosystem runtime layer. RegiMux normalizes them into typed runtime entries with an ecosystem kind, alias, registry, mirrors, probe settings, auth, and HTTP policy. The scheduler then works from runtime capabilities such as `probe` and `prefetch` instead of reading a legacy `upstreams` block.

Container runtimes expose scheduled `probe` and predictive `prefetch` capabilities. Go, npm, PyPI, and Maven expose the shared endpoint `probe` capability when an alias has `probe.enabled = true`, and they also participate in scheduled `prefetch` by rewarming recently requested artifacts.

RegiMux disables HTTP/2 for upstream registry clients by default. This keeps mirror and CDN compatibility predictable and avoids process-level HTTP/2 runtime panics. Enable it per upstream only for trusted registries:

```hcl
container {
  hub {
    http {
      http2 {
        enabled = true
      }
    }
  }
}
```

`cache.blob.stream_and_cache` is enabled by default. Full blob misses stream back to Docker while the same bytes are written into object storage; the cache and metadata are committed only after the client completes the blob stream. HEAD requests do not store blob bytes. Range misses fill the full blob into object storage first, then serve the requested range from the local object.

Admin `Committed Blob Bytes (metadata)` is derived from committed blob metadata, not from a live scan of `store.object` or from bytes merely passing through the proxy. `cache.backend` is a KV cache backend and is separate from the object store configured by `store.object`.

## Dependency Policy

Use `policy.dependency` to block or allow specific artifact requests before RegiMux calls any upstream.

- `dependency.block` has highest priority and is evaluated before `dependency.allow`.
- If `dependency.allow` is non-empty, requests must match at least one allow rule; otherwise they are denied.
- If `dependency.allow` is empty, all requests are allowed by default unless a block rule matches.
- Empty rule fields are wildcards (`*` style). Field values are trimmed, and ecosystem names are normalized case-insensitively.
- Field matching supports exact match or prefix wildcard (`*`) in `ecosystem`, `alias`, `artifact`, and `reference` values.

Container and Go/npm/PyPI/Maven fields are taken from the request route:

- `ecosystem`: `container`, `go`, `npm`, `pypi`, `maven`
- `alias`: upstream alias from the request path
- `artifact`:
  - container: repository (for example `library/alpine`)
  - go: module path (for example `github.com/example/mod`)
  - npm: package name
  - pypi: normalized repository path (for example `pypi/simple/requests`)
  - maven: artifact directory path
- `reference`:
  - container: tag / digest / `tags` marker depending on route type
  - go: artifact ref tail (for example `@v/v1.2.3.zip`)
  - npm: `metadata`, `tarball:...`, or `path:...`
  - pypi: `index.html` or normalized package path
  - maven: file name

Denied requests return `403 Forbidden` and do not fetch from upstream. They are still recorded in pull metadata with a separate policy-denied counter and last-denied timestamp, so admin activity can show blocked dependency proxy traffic without counting it as a successful pull. Prometheus exposes dependency proxy pull outcomes through `regimux_service_dependency_proxy_pulls_total{ecosystem,kind,alias,repository,status}` and policy denials through `regimux_service_dependency_proxy_policy_denied_pulls_total{ecosystem,kind,alias,repository}`.

```hcl
policy {
  dependency {
    allow {
      ecosystem = "go"
      alias = "default"
      artifact = "github.com/example/*"
      reference = "v1.2.3"
    }

    block {
      ecosystem = "container"
      alias = "hub"
      artifact = "private/*"
      reference = "*"
    }
  }
}
```

Small blob caching can store already-verified tiny blobs, such as OCI image config blobs, in the configured KV cache backend. Use Redis or Valkey for this mode; large layers still belong in `store.object`.

```hcl
cache {
  backend = "redis"

  blob {
    small_cache {
      enabled = true
      max_size_bytes = 4194304
      ttl = "24h"
    }
  }
}
```

## Docker Daemon Integration

The `docker` block is optional and disabled by default. When enabled, RegiMux connects to the host Docker daemon through the Docker socket. It can observe local image events and ask the host daemon to pull configured images through the RegiMux proxy after startup, warming the RegiMux cache.

The container runtime must explicitly mount the socket, for example `/var/run/docker.sock:/var/run/docker.sock` on Linux Docker Engine. With Docker Desktop, set `prewarm.registry` to an address reachable by the Docker daemon, such as `192.168.1.2:5000`, instead of container-local `localhost:5000`.

```hcl
docker {
  enabled = true
  observe = true

  prewarm {
    enabled = true
    registry = "192.168.1.2:5000"
    alias = "hub"
    images = ["alpine:latest", "library/nginx:1.27"]
    timeout = "10m"
  }
}
```

## Environment

Environment variables use `REGIMUX_` and `__` for nesting:

```text
REGIMUX_SERVER__LISTEN=:5000
REGIMUX_SERVER__PUBLIC_URL=http://localhost:5000
REGIMUX_LOG__LEVEL=debug
REGIMUX_LOG__DEBUG=true
REGIMUX_SERVER__MIDDLEWARE__REQUEST_LOGGER__ENABLED=true
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
REGIMUX_CACHE__BLOB__SMALL_CACHE__ENABLED=true
REGIMUX_DOCKER__ENABLED=true
REGIMUX_DOCKER__PREWARM__REGISTRY=192.168.1.2:5000
REGIMUX_SCHEDULER__MANIFEST_REFRESH__ECOSYSTEMS__CONTAINER__ENABLED=true
REGIMUX_SCHEDULER__MANIFEST_REFRESH__ECOSYSTEMS__CONTAINER__INTERVAL=10m
REGIMUX_CONTAINER__HUB__REGISTRY=https://registry-1.docker.io
REGIMUX_CONTAINER__HUB__HTTP__HTTP2__ENABLED=true
REGIMUX_GO__DEFAULT__REGISTRY=https://proxy.golang.org
REGIMUX_NPM__DEFAULT__REGISTRY=https://registry.npmjs.org
REGIMUX_PYPI__DEFAULT__REGISTRY=https://pypi.org
REGIMUX_MAVEN__CENTRAL__REGISTRY=https://repo.maven.apache.org/maven2
```

The loader also reads `.env` when present. Environment variables override `.env` and file values.

## Command-line Overrides

Unknown Cobra flags are passed to `configx` as config overrides:

```bash
regimuxd --config /etc/regimux/regimux.hcl --server.listen=:5000 --log.level=debug
```

`log.debug = true` and `REGIMUX_LOG__DEBUG=true` are accepted as compatibility aliases for setting debug logging, but `log.level = "debug"` is the preferred form.

Use this for small operational overrides. Keep larger configuration in HCL or environment variables.

## Validation

Config validation rejects invalid enum values, invalid URLs, negative durations/counts, invalid cleanup watermarks, unsupported store drivers, incomplete S3/SFTP credentials, and Docker prewarm configs pointing at an unknown container alias.

Supported metadata drivers:

- `sqlite`
- `mysql`
- `postgres`
- `postgresql` (alias of `postgres`)

Supported object drivers:

- `local`
- `memory`
- `s3`
- `sftp`
