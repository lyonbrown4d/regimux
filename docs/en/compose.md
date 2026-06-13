# Docker Compose

Compose examples live under [examples/compose](../../examples/compose). They use the released GHCR image by default:

```text
ghcr.io/lyonbrown4d/regimux:latest
```

Copy the environment template when you want to pin a release, change the port, or override runtime config:

```bash
cp examples/compose/.env.example examples/compose/.env
```

## Examples

Single-node memory cache:

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.memory.yml up -d
```

Redis cache and distributed scheduler lock:

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.redis.yml up -d
```

Valkey cache and distributed scheduler lock:

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.valkey.yml up -d
```

Prometheus scraping:

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.observability.yml up -d
```

The bundled Grafana dashboard includes dependency proxy pull panels for `regimux_service_dependency_proxy_pulls_total` and policy-denied pull panels for `regimux_service_dependency_proxy_policy_denied_pulls_total`. Use them to compare pull traffic by ecosystem, kind, alias, repository, and status, and to spot dependency policy blocks before they reach an upstream.

## Pin a Release

Set this in `examples/compose/.env`:

```text
REGIMUX_IMAGE=ghcr.io/lyonbrown4d/regimux:v0.0.2
REGIMUX_HTTP_PORT=8080
```

For Debian:

```text
REGIMUX_IMAGE=ghcr.io/lyonbrown4d/regimux:v0.0.2-debian
```

## Runtime Overrides

Compose passes `REGIMUX_*` variables into the container. RegiMux loads them through `configx`:

```text
REGIMUX_SERVER__PUBLIC_URL=http://localhost:8080
REGIMUX_LOG__LEVEL=debug
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
```

For more detail, see the source-level example notes in [examples/compose/README.md](../../examples/compose/README.md).
