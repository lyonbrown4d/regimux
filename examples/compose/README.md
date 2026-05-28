# Docker Compose examples

This directory contains runnable Docker Compose examples for RegiMux.

Copy the environment template if you want to pin a release image or change the host port:

```bash
cp examples/compose/.env.example examples/compose/.env
```

## Single-node memory cache

Use this for local testing or a small single-node deployment. Manifest, tag, and referrer metadata are cached in memory; blob data and pull metadata are stored in the `regimux-data` Docker volume.

```bash
docker compose --env-file examples/compose/.env.example -f examples/compose/compose.memory.yml up -d
curl -i http://localhost:5000/healthz
docker pull localhost:5000/hub/library/alpine:latest
```

## Redis cache and distributed scheduler lock

Use this when cache metadata should survive RegiMux restarts and scheduler jobs should use Redis-backed distributed locking.

```bash
docker compose --env-file examples/compose/.env.example -f examples/compose/compose.redis.yml up -d
curl -i http://localhost:5000/v2/
docker pull localhost:5000/hub/library/nginx:latest
```

This example also enables predictive prefetch and latency-based mirror selection for Docker Hub blobs.

## Valkey cache and distributed scheduler lock

Use this if you prefer Valkey instead of Redis. RegiMux still uses the Redis-compatible lock protocol for scheduler coordination.

```bash
docker compose --env-file examples/compose/.env.example -f examples/compose/compose.valkey.yml up -d
curl -i http://localhost:5000/v2/
docker pull localhost:5000/hub/library/busybox:latest
```

## Prometheus scraping

The observability example starts RegiMux and Prometheus. RegiMux exposes Prometheus metrics at `/metrics`.

```bash
docker compose --env-file examples/compose/.env.example -f examples/compose/compose.observability.yml up -d
```

Prometheus is available at `http://localhost:9090`.

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

## Paths

Inside the container:

- `/etc/regimux/regimux.hcl` is the config file.
- `/var/lib/regimux` is the working directory and persistent data root.
- `data/regimux.db` stores local metadata when using the bboltx meta store.
- `data/objects` stores local blob objects.

The Compose examples mount `/var/lib/regimux` as a named volume, so cached blobs and pull metadata survive container recreation.

## Notes for replicas

Redis or Valkey shares the byte cache and scheduler locks, but the current examples still use local bboltx metadata and local blob object storage per RegiMux container. Do not scale these Compose files with multiple RegiMux replicas sharing the same `/var/lib/regimux` volume. If you run multiple replicas, give each replica its own local data volume and put them behind an external load balancer.
