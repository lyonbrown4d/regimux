# Usage

RegiMux is published through [GitHub Releases](https://github.com/lyonbrown4d/regimux/releases) and GitHub Container Registry. The recommended entry point is a released artifact, not a source checkout.

## Dependency Proxy Model

Run RegiMux close to your developers, CI runners, or build cluster, then point dependency clients at RegiMux instead of every public upstream:

- Docker/containerd uses the Registry-compatible `/v2/{containerAlias}/...` path.
- Go uses `GOPROXY=http://<regimux>/go/{goAlias}`.
- npm, PyPI, and Maven use their ecosystem proxy paths under `/npm/{npmAlias}`, `/pypi/{pypiAlias}`, and `/maven/{mavenAlias}`.
- Binary distributions, such as Gradle wrapper zips or CLI installers, use the dist ecosystem proxy path `/dist/{distAlias}/{path}`.

RegiMux is read-only. It proxies dependency reads, caches immutable artifacts, keeps metadata for cache accounting and cleanup, and can warm or refresh artifacts in the background. It is not a package publishing endpoint and it is not a push registry.

## Docker

The default image is Alpine-based:

```bash
docker run --rm \
  -p 8080:8080 \
  -v regimux-data:/var/lib/regimux \
  ghcr.io/lyonbrown4d/regimux:latest
```

Pinned release images are also published:

```bash
docker run --rm \
  -p 8080:8080 \
  -v regimux-data:/var/lib/regimux \
  ghcr.io/lyonbrown4d/regimux:v0.0.2
```

Debian-based images use the `-debian` suffix:

```bash
docker run --rm \
  -p 8080:8080 \
  -v regimux-data:/var/lib/regimux \
  ghcr.io/lyonbrown4d/regimux:v0.0.2-debian
```

The container reads `/etc/regimux/regimux.hcl`, uses `/var/lib/regimux` as its working directory, and listens on `:8080` by default.

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
curl -i http://localhost:8080/livez
curl -i http://localhost:8080/readyz
curl -i http://localhost:8080/v2/
```

## Container Images

RegiMux uses the first repository path segment as the container alias:

```text
localhost:8080/{containerAlias}/library/alpine:latest
localhost:8080/{containerAlias}/org/app:v1.2.3
localhost:8080/{containerAlias}/coreos/etcd:v3.5.0
```

Pull images through the container dependency proxy:

```bash
docker pull localhost:8080/{containerAlias}/library/alpine:latest
```

Fetch a manifest directly:

```bash
curl -i \
  -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
  http://localhost:8080/v2/{containerAlias}/library/alpine/manifests/latest
```

## Go Module Proxy

Each alias under the `go` block exposes a Go module proxy endpoint at `/go/{goAlias}`. Go clients use RegiMux as their dependency proxy by setting `GOPROXY`:

```bash
export GOPROXY=http://localhost:8080/go/{goAlias},direct
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

The selected Go alias is resolved only within the `go` block. Container, npm, PyPI, Maven, and dist aliases use their own namespaces.

`@latest` and `@v/list` use a short TTL. Versioned `.info`, `.mod`, and `.zip` responses are stored in the object store by content sha256 and reused long term. The current implementation is a read-only Go dependency proxy; it does not proxy `sum.golang.org` and does not perform VCS direct fetching.

## Dist Mirror

Each alias under the `dist` block exposes a generic file download mirror at `/dist/{distAlias}/{path}`. RegiMux does not need package-specific code for Gradle, Electron, CLI installers, or other release assets; each one is just a configured alias with an upstream, optional mirrors, and path allow rules. The default `gradle` alias points at `https://services.gradle.org/distributions` and allows Gradle wrapper archives:

```text
GET /dist/gradle/gradle-8.7-bin.zip
GET /dist/gradle/gradle-8.7-all.zip
```

Configure additional mirrors when you have an internal cache or regional distribution endpoint:

```hcl
dist {
  gradle {
    registry = "https://services.gradle.org/distributions"
    mirrors = ["https://dist-cache.example.com/gradle"]
    mirror_policy = "ordered"
    allow = ["gradle-*-bin.zip", "gradle-*-all.zip"]
  }

  electron {
    registry = "https://github.com/electron/electron/releases/download"
    mirrors = ["https://dist-cache.example.com/electron"]
    mirror_policy = "ordered"
    allow = [
      "v*/electron-v*",
      "v*/SHASUMS256.txt",
      "v*/SHASUMS256.txt.sig",
    ]
  }

  playwright {
    registry = "https://cdn.playwright.dev"
    mirrors = ["https://dist-cache.example.com/playwright"]
    mirror_policy = "ordered"
    allow = ["builds/*", "dbazure/download/playwright/*"]
  }

  cypress {
    registry = "https://download.cypress.io"
    mirrors = ["https://dist-cache.example.com/cypress"]
    mirror_policy = "ordered"
    allow = ["desktop", "desktop.json", "desktop/*"]
  }

  nodejs {
    registry = "https://nodejs.org/download/release"
    mirrors = ["https://dist-cache.example.com/nodejs"]
    mirror_policy = "ordered"
    allow = ["v*/node-v*", "index.json", "index.tab"]
  }

  hashicorp {
    registry = "https://releases.hashicorp.com"
    mirrors = ["https://dist-cache.example.com/hashicorp"]
    mirror_policy = "ordered"
    allow = ["terraform/*", "vault/*", "consul/*", "nomad/*"]
  }
}
```

Use it from `gradle/wrapper/gradle-wrapper.properties`:

```properties
distributionUrl=http\://localhost\:8080/dist/gradle/gradle-8.7-bin.zip
```

For Electron installed through npm, npm downloads the package from the npm registry first, then Electron's install path uses `@electron/get` to download release artifacts. `@electron/get` builds URLs from a mirror base, version directory, and artifact file name; it reads mirror settings from `.npmrc`, package config, or environment variables such as `ELECTRON_MIRROR`. Point that mirror base at a dist alias:

```ini
electron_mirror=http://localhost:8080/dist/electron/
```

or:

```bash
export ELECTRON_MIRROR=http://localhost:8080/dist/electron/
npm install electron
```

Other common clients expose similar download-base settings:

```bash
PLAYWRIGHT_DOWNLOAD_HOST=http://localhost:8080/dist/playwright npx playwright install
CYPRESS_DOWNLOAD_MIRROR=http://localhost:8080/dist/cypress cypress install
npm_config_disturl=http://localhost:8080/dist/nodejs npm rebuild
```

For release sites without a built-in mirror setting, use the dist URL directly in CI scripts, for example:

```bash
curl -LO http://localhost:8080/dist/hashicorp/terraform/1.9.0/terraform_1.9.0_linux_amd64.zip
```

Full `GET` responses are stored in the object store by content sha256 and linked to metadata for cache accounting and cleanup. `HEAD` requests do not store bytes. `Range` requests are served from the local object when the full artifact is already cached; range misses are passed through to upstream and are not stored as partial objects.

When a dist endpoint fails with a transport error or returns `404`, `410`, `408`, `429`, or `5xx`, RegiMux tries the next configured mirror when available. Non-retryable responses such as `403` are returned directly.

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
http://localhost:8080/admin
```

The Admin UI is embedded in the binary. It includes dashboard, upstream health, pulls, activity, cache, storage, scheduler, manual refresh, auth audit, and effective config views.

Manual refresh is ecosystem-aware and runs as a background job:

```text
container:hub / repository=library/node / reference=20
go:default / repository=github.com/pkg/errors / reference=v0.9.1
dist:gradle / repository=dist / reference=gradle-8.7-bin.zip
```

When `auth.enabled = true`, `/admin` is protected with HTTP Basic using the same configured users as Registry auth.

## Configuration Overrides

RegiMux reads typed HCL config, dotenv, environment variables, and command-line overrides through `configx`.

Environment variables use the `REGIMUX_` prefix and `__` for nesting:

```text
REGIMUX_SERVER__PUBLIC_URL=http://localhost:8080
REGIMUX_LOG__LEVEL=debug
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
```

Command-line overrides use dotted keys:

```bash
regimuxd --config /etc/regimux/regimux.hcl --server.listen=:8080 --log.level=debug
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
