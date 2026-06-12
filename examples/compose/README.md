# Docker Compose examples

This directory contains runnable Docker Compose examples for RegiMux.

Copy the environment template if you want to pin a release image, change the host port, or override RegiMux runtime config with environment variables:

```bash
cp examples/compose/.env.example examples/compose/.env
```

Each RegiMux service reads `.env.example` and then optional `.env` with Docker Compose `env_file`. Any `REGIMUX_*` runtime config variable in `.env` is passed into the container and loaded by `configx`.

## Single-node memory cache

Use this for local testing or a small single-node deployment. Manifest, tag, and referrer metadata are cached in memory; blob data and pull metadata are stored in the `regimux-data` Docker volume.

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.memory.yml up -d
curl -i http://localhost:5000/livez
docker pull localhost:5000/hub/library/alpine:latest
```

## Redis cache and distributed scheduler lock

Use this when cache metadata should survive RegiMux restarts and scheduler jobs should use Redis-backed distributed locking.

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.redis.yml up -d
curl -i http://localhost:5000/v2/
docker pull localhost:5000/hub/library/nginx:latest
```

This example also enables predictive prefetch and latency-based mirror selection for Docker Hub blobs.

## Go module proxy

The Compose configs include a `golang` upstream for `https://proxy.golang.org`. After starting any example, point Go at RegiMux:

```bash
export GOPROXY=http://localhost:5000,direct
go mod download github.com/pkg/errors@v0.9.1
```

Go module proxy responses are cached in the same object store used for OCI blobs. The root Go proxy path tries configured `type = "go"` upstreams in stable alias order, preferring the `golang` alias when present. Use `/go/{alias}/...` only when you need to target one Go upstream explicitly.

## Valkey cache and distributed scheduler lock

Use this if you prefer Valkey instead of Redis. RegiMux still uses the Redis-compatible lock protocol for scheduler coordination.

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.valkey.yml up -d
curl -i http://localhost:5000/v2/
docker pull localhost:5000/hub/library/busybox:latest
```

## Prometheus and Grafana

The observability example starts RegiMux, Prometheus, and Grafana. RegiMux exposes Prometheus metrics at `/metrics`, and Grafana auto-loads the `RegiMux Overview` dashboard from `examples/compose/grafana/dashboards/regimux-overview.json`.

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.observability.yml up -d
```

The dashboard includes dependency proxy pull panels backed by:

- `regimux_service_dependency_proxy_pulls_total{ecosystem,kind,alias,repository,status}` for dependency proxy pull outcomes.
- `regimux_service_dependency_proxy_policy_denied_pulls_total{ecosystem,kind,alias,repository}` for requests rejected by `policy.dependency` before any upstream fetch.

Useful PromQL checks:

```promql
sum by (ecosystem, kind, alias, status) (
  rate(regimux_service_dependency_proxy_pulls_total[$__rate_interval])
)

topk(20, sum by (ecosystem, kind, alias, repository) (
  increase(regimux_service_dependency_proxy_policy_denied_pulls_total[$__range])
))
```

Prometheus is available at `http://localhost:9090`.
Grafana is available at `http://localhost:3000`; anonymous read-only access is enabled for local use. The default admin password is `regimux` unless `GRAFANA_ADMIN_PASSWORD` is set in `.env`.

## Optional Docker daemon integration

RegiMux can connect to the host Docker daemon when you explicitly mount the Docker socket. This is off by default because the socket grants broad control over the host Docker daemon.

For Linux Docker Engine, add this mount to the `regimux` service:

```yaml
volumes:
  - /var/run/docker.sock:/var/run/docker.sock
```

Then enable the runtime config in `.env` or HCL:

```text
REGIMUX_DOCKER__ENABLED=true
REGIMUX_DOCKER__OBSERVE=true
REGIMUX_DOCKER__PREWARM__ENABLED=true
REGIMUX_DOCKER__PREWARM__REGISTRY=192.168.1.2:5000
REGIMUX_DOCKER__PREWARM__ALIAS=hub
```

`docker.prewarm.registry` must be reachable by the Docker daemon itself. On Docker Desktop that is often the host LAN IP, not `localhost`.

## S3-compatible object storage

RegiMux can store blob objects in an S3-compatible backend while keeping metadata in SQLite, MySQL, or PostgreSQL. Set these values in `examples/compose/.env` or a production environment:

```text
REGIMUX_STORE__OBJECT__DRIVER=s3
REGIMUX_STORE__OBJECT__S3__BUCKET=regimux-objects
REGIMUX_STORE__OBJECT__S3__PREFIX=cache
REGIMUX_STORE__OBJECT__S3__REGION=us-east-1
REGIMUX_STORE__OBJECT__S3__ENDPOINT=http://minio:9000
REGIMUX_STORE__OBJECT__S3__ACCESS_KEY_ID=regimux
REGIMUX_STORE__OBJECT__S3__SECRET_ACCESS_KEY=change-me
REGIMUX_STORE__OBJECT__S3__FORCE_PATH_STYLE=true
```

For AWS S3, leave `endpoint` empty and use the normal AWS credential chain or explicit access keys.

## SFTP object storage

RegiMux can also store blob objects on a shared SFTP server. Set these values in `examples/compose/.env` or a production environment:

```text
REGIMUX_STORE__OBJECT__DRIVER=sftp
REGIMUX_STORE__OBJECT__PATH=/srv/regimux/objects
REGIMUX_STORE__OBJECT__SFTP__ADDR=sftp.example.com:22
REGIMUX_STORE__OBJECT__SFTP__USERNAME=regimux
REGIMUX_STORE__OBJECT__SFTP__PASSWORD=change-me
REGIMUX_STORE__OBJECT__SFTP__KNOWN_HOSTS_PATH=/etc/regimux/known_hosts
REGIMUX_STORE__OBJECT__SFTP__TIMEOUT=10s
```

Use `REGIMUX_STORE__OBJECT__SFTP__HOST_KEY` instead of `known_hosts_path` when you want to pin one host public key directly from the environment.

## Cleanup capacity control

The cleanup job removes blobs that have not been accessed for `scheduler.cleanup.unused_for`. You can also set object-cache byte watermarks through environment variables:

```text
REGIMUX_SCHEDULER__CLEANUP__MAX_BYTES=10737418240
REGIMUX_SCHEDULER__CLEANUP__TARGET_BYTES=8589934592
```

When metadata-reported blob bytes exceed `max_bytes`, RegiMux evicts the least recently accessed unprotected blobs until it reaches `target_bytes` or the scan/delete limits.

## Image tags

The examples default to:

```text
ghcr.io/lyonbrown4d/regimux:latest
```

For a pinned release, put this in `examples/compose/.env`:

```text
REGIMUX_IMAGE=ghcr.io/lyonbrown4d/regimux:v0.0.2
REGIMUX_HTTP_PORT=5000
```

Debian-based images are also published:

```text
ghcr.io/lyonbrown4d/regimux:v0.0.2-debian
ghcr.io/lyonbrown4d/regimux:latest-debian
```

## Runtime config environment

RegiMux config paths can be overridden with `REGIMUX_` environment variables. Use `__` for nesting:

```text
REGIMUX_SERVER__PUBLIC_URL=http://localhost:5000
REGIMUX_LOG__LEVEL=debug
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
REGIMUX_UPSTREAMS__HUB__REGISTRY=https://registry-1.docker.io
```

For auth in Compose, add values like this to `examples/compose/.env`:

```text
REGIMUX_AUTH__ENABLED=true
REGIMUX_AUTH__TOKEN_SECRET=replace-me
REGIMUX_AUTH__USERS__ALICE__PASSWORD=secret
REGIMUX_AUTH__USERS__ALICE__REPOSITORIES=hub/*
```

## Paths

Inside the container:

- `/etc/regimux/regimux.hcl` is the config file.
- `/var/lib/regimux` is the working directory and persistent data root.
- `data/regimux.db` stores local SQLite metadata when `store.meta.driver = "sqlite"`.
- `data/objects` stores local blob objects when `store.object.driver = "local"`.

The Compose examples mount `/var/lib/regimux` as a named volume, so cached blobs and pull metadata survive container recreation.

## Notes for replicas

Redis or Valkey shares the byte cache and scheduler locks, but the default Compose files still use local SQLite metadata and local blob object storage per RegiMux container. Do not scale these Compose files with multiple RegiMux replicas sharing the same `/var/lib/regimux` volume. If you run multiple replicas, use `store.meta.driver = "mysql"` or `"postgres"` with a shared DSN and `store.object.driver = "s3"` or `"sftp"` with shared object storage, then put the replicas behind an external load balancer.
