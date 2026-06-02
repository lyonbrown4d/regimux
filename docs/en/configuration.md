# Configuration

RegiMux uses typed HCL config loaded through `configx`. The release packages and container image include a minimal config at `/etc/regimux/regimux.hcl`.

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

upstreams {
  hub {
    registry = "https://registry-1.docker.io"
  }
}
```

## Defaults

Defaults are defined as typed Go config, then validated after file, dotenv, environment, and command-line sources are merged.

Important defaults:

- `server.listen = ":5000"`
- `server.public_url = "http://localhost:5000"`
- `server.middleware.request_id.enabled = true`
- `server.middleware.healthcheck.enabled = true` with `/livez` and `/readyz`
- `server.middleware.etag.enabled = true`, scoped away from registry `/v2` traffic
- `server.middleware.compress.enabled = true`, scoped away from registry `/v2` traffic
- `server.middleware.security_headers.enabled = true`, scoped away from registry `/v2` traffic
- `server.middleware.security_headers.cross_origin_embedder_policy = "unsafe-none"` so the embedded admin UI can load CDN assets
- `server.middleware.rate_limit.enabled = false`
- `server.middleware.csrf.enabled = false`
- `server.middleware.pprof.enabled = false`
- `cache.backend = "memory"`
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
- `docker.enabled = false`
- `docker.observe = true`
- `docker.prewarm.alias = "hub"`
- `docker.prewarm.timeout = "10m"`
- `upstreams.hub.registry = "https://registry-1.docker.io"`
- `upstreams.hub.http.http2.enabled = false`

RegiMux disables HTTP/2 for upstream registry clients by default. This keeps mirror and CDN compatibility predictable and avoids process-level HTTP/2 runtime panics. Enable it per upstream only for trusted registries:

```hcl
upstreams {
  hub {
    http {
      http2 {
        enabled = true
      }
    }
  }
}
```

`cache.blob.stream_and_cache` is enabled by default. Full blob misses stream back to Docker while the same bytes are written into object storage; the cache and metadata are committed after the stream completes. Range requests pass through upstream until a full blob has been cached.

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
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
REGIMUX_CACHE__BLOB__SMALL_CACHE__ENABLED=true
REGIMUX_DOCKER__ENABLED=true
REGIMUX_DOCKER__PREWARM__REGISTRY=192.168.1.2:5000
REGIMUX_UPSTREAMS__HUB__REGISTRY=https://registry-1.docker.io
REGIMUX_UPSTREAMS__HUB__HTTP__HTTP2__ENABLED=true
```

The loader also reads `.env` when present. Environment variables override `.env` and file values.

## Command-line Overrides

Unknown Cobra flags are passed to `configx` as config overrides:

```bash
regimuxd --config /etc/regimux/regimux.hcl --server.listen=:5000 --log.level=debug
```

Use this for small operational overrides. Keep larger configuration in HCL or environment variables.

## Validation

Config validation rejects invalid enum values, invalid URLs, negative durations/counts, invalid cleanup watermarks, unsupported store drivers, incomplete S3/SFTP credentials, and Docker prewarm configs pointing at an unknown upstream alias.

Supported metadata drivers:

- `sqlite`
- `mysql`
- `postgres`

Supported object drivers:

- `local`
- `memory`
- `s3`
- `sftp`
