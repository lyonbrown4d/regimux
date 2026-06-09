# Usage

RegiMux is published through [GitHub Releases](https://github.com/lyonbrown4d/regimux/releases) and GitHub Container Registry. The recommended entry point is a released artifact, not a source checkout.

## Docker

The default image is Alpine-based:

```bash
docker run --rm \
  -p 5000:5000 \
  -v regimux-data:/var/lib/regimux \
  ghcr.io/lyonbrown4d/regimux:latest
```

Pinned release images are also published:

```bash
docker run --rm \
  -p 5000:5000 \
  -v regimux-data:/var/lib/regimux \
  ghcr.io/lyonbrown4d/regimux:v0.0.2
```

Debian-based images use the `-debian` suffix:

```bash
docker run --rm \
  -p 5000:5000 \
  -v regimux-data:/var/lib/regimux \
  ghcr.io/lyonbrown4d/regimux:v0.0.2-debian
```

The container reads `/etc/regimux/regimux.hcl`, uses `/var/lib/regimux` as its working directory, and listens on `:5000` by default.

## deb and rpm

Download the matching package from the [GitHub Release page](https://github.com/lyonbrown4d/regimux/releases):

```bash
sudo dpkg -i ./regimuxd_*_linux_amd64.deb
sudo systemctl enable --now regimuxd
```

For rpm-based systems:

```bash
sudo rpm -Uvh ./regimuxd_*_linux_amd64.rpm
sudo systemctl enable --now regimuxd
```

Packages install:

- `/usr/bin/regimuxd`
- `/etc/regimux/regimux.hcl`
- `/lib/systemd/system/regimuxd.service`
- `/var/lib/regimux`

## Archives and Windows exe

GitHub Releases include tar archives for Linux and macOS, zip archives for Windows, and a standalone Windows binary artifact.

Linux example:

```bash
tar -xzf regimux_0.0.2_linux_amd64.tar.gz
./regimuxd --config configs/regimux.minimal.hcl
```

Windows example:

```powershell
Expand-Archive .\regimux_0.0.2_windows_amd64.zip
.\regimux_0.0.2_windows_amd64\regimuxd.exe --config .\regimux_0.0.2_windows_amd64\configs\regimux.minimal.hcl
```

## Health Checks

```bash
curl -i http://localhost:5000/livez
curl -i http://localhost:5000/readyz
curl -i http://localhost:5000/v2/
```

## Pull Images

RegiMux uses the first repository path segment as the container alias:

```text
localhost:5000/{containerAlias}/library/alpine:latest
localhost:5000/{containerAlias}/org/app:v1.2.3
localhost:5000/{containerAlias}/coreos/etcd:v3.5.0
```

Pull through Docker:

```bash
docker pull localhost:5000/{containerAlias}/library/alpine:latest
```

Fetch a manifest directly:

```bash
curl -i \
  -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
  http://localhost:5000/v2/{containerAlias}/library/alpine/manifests/latest
```

## Go Module Proxy

Each alias under the `go` block exposes a Go module proxy endpoint at `/go/{goAlias}`. Go clients can use RegiMux as a Go module proxy:

```bash
export GOPROXY=http://localhost:5000/go/{goAlias},direct
go env GOPROXY
go mod download github.com/pkg/errors@v0.9.1
```

RegiMux proxies and caches Go proxy protocol requests such as:

```text
GET /go/{goAlias}/github.com/pkg/errors/@v/list
GET /go/{goAlias}/github.com/pkg/errors/@v/v0.9.1.info
GET /go/{goAlias}/github.com/pkg/errors/@v/v0.9.1.mod
GET /go/{goAlias}/github.com/pkg/errors/@v/v0.9.1.zip
GET /go/{goAlias}/github.com/pkg/errors/@latest
```

The selected Go alias is resolved only within the `go` block. Container, npm, PyPI, and Maven aliases use their own namespaces.

`@latest` and `@v/list` use a short TTL. Versioned `.info`, `.mod`, and `.zip` responses are stored in the object store by content sha256 and reused long term. The current implementation is a read-only read-through proxy; it does not proxy `sum.golang.org` and does not perform VCS direct fetching.

## Docker Compose

Compose examples use the released GHCR image by default:

```bash
cp examples/compose/.env.example examples/compose/.env
docker compose --env-file examples/compose/.env -f examples/compose/compose.memory.yml up -d
```

Other examples:

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.redis.yml up -d
docker compose --env-file examples/compose/.env -f examples/compose/compose.valkey.yml up -d
docker compose --env-file examples/compose/.env -f examples/compose/compose.observability.yml up -d
```

More details: [Compose examples](../../examples/compose/README.md).

## Admin UI

Open:

```text
http://localhost:5000/admin
```

The Admin UI is embedded in the binary. It includes dashboard, upstream health, pulls, activity, cache, storage, scheduler, manual refresh, auth audit, and effective config views.

Manual refresh is ecosystem-aware and runs as a background job:

```text
container:hub / repository=library/node / reference=20
go:default / repository=github.com/pkg/errors / reference=v0.9.1
```

When `auth.enabled = true`, `/admin` is protected with HTTP Basic using the same configured users as Registry auth.

## Configuration Overrides

RegiMux reads typed HCL config, dotenv, environment variables, and command-line overrides through `configx`.

Environment variables use the `REGIMUX_` prefix and `__` for nesting:

```text
REGIMUX_SERVER__PUBLIC_URL=http://localhost:5000
REGIMUX_LOG__LEVEL=debug
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
```

Command-line overrides use dotted keys:

```bash
regimuxd --config /etc/regimux/regimux.hcl --server.listen=:5000 --log.level=debug
```

## Development

For local development from a source checkout:

```bash
go run ./cmd/regimuxd --config configs/regimux.minimal.hcl
```

Common next steps:

- Change runtime config: [Configuration](configuration.md).
- Enable auth and `docker login`: [Authentication](auth.md).
- Use Redis, Valkey, S3, SFTP, MySQL, or PostgreSQL: [Storage](storage.md).
- Tune cleanup and prefetch: [Scheduler](scheduler.md).
