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
- `server.middleware.rate_limit.enabled = false`
- `server.middleware.csrf.enabled = false`
- `server.middleware.pprof.enabled = false`
- `cache.backend = "memory"`
- `store.meta.driver = "sqlite"`
- `store.meta.path = "data/regimux.db"`
- `store.object.driver = "local"`
- `store.object.path = "data/objects"`
- `scheduler.cleanup.enabled = true`
- `scheduler.prefetch.enabled = false`
- `upstreams.hub.registry = "https://registry-1.docker.io"`

## Environment

Environment variables use `REGIMUX_` and `__` for nesting:

```text
REGIMUX_SERVER__LISTEN=:5000
REGIMUX_SERVER__PUBLIC_URL=http://localhost:5000
REGIMUX_LOG__LEVEL=debug
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
REGIMUX_UPSTREAMS__HUB__REGISTRY=https://registry-1.docker.io
```

The loader also reads `.env` when present. Environment variables override `.env` and file values.

## Command-line Overrides

Unknown Cobra flags are passed to `configx` as config overrides:

```bash
regimuxd --config /etc/regimux/regimux.hcl --server.listen=:5000 --log.level=debug
```

Use this for small operational overrides. Keep larger configuration in HCL or environment variables.

## Validation

Config validation rejects invalid enum values, invalid URLs, negative durations/counts, invalid cleanup watermarks, unsupported store drivers, and incomplete S3/SFTP credentials.

Supported metadata drivers:

- `sqlite`
- `mysql`
- `postgres`

Supported object drivers:

- `local`
- `memory`
- `s3`
- `sftp`
