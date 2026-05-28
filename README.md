# RegiMux

RegiMux is a read-only OCI / Docker Registry V2 multi-upstream proxy mirror gateway.

This repository currently contains a runnable skeleton based on the design document:

- `regimuxd`: single-process daemon entrypoint.
- Cobra-based command line with a single config-driven daemon mode.
- Strongly typed `httpx.Endpoint` routes for health, Registry V2 ping, manifests, blobs, tags, and referrers.
- Alias-based upstream routing such as `/v2/hub/library/alpine/manifests/latest`.
- One alias can fan out to multiple Docker Hub mirrors with ordered failover or round-robin starting points.
- Upstream registry client based on `github.com/arcgolabs/clientx/http` with bearer-token challenge handling.
- Manifest cache backed by memory/Redis/Valkey plus dbx metadata storage and local object storage.
- Blob cache-then-serve path with local CAS storage, digest verification, range reads, and repo-to-blob access links.
- Tags/list and referrers response caching, including tags Link header rewrite and OCI referrers fallback tag support.
- Embedded Fiber admin UI at `/admin` for upstream health, pull activity, cache metadata, scheduler config, and effective config.
- DI and lifecycle wiring with `github.com/arcgolabs/dix`, including endpoint collection injection into the HTTP server.
- Config loading with `github.com/arcgolabs/configx`.
- Logging with `github.com/arcgolabs/logx` on top of `log/slog`.
- Event bus wiring with `github.com/arcgolabs/eventx`.
- `collectionx` usage for ordered upstream registry snapshots.
- Storage uses `github.com/arcgolabs/dbx` repositories for SQLite, MySQL, or PostgreSQL metadata and local, memory, S3-compatible, or SFTP object storage.

Run locally:

```bash
go run ./cmd/regimuxd --config configs/regimux.minimal.hcl
```

Then try:

```bash
curl -i http://localhost:5000/v2/
curl -i -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
  http://localhost:5000/v2/hub/library/alpine/manifests/latest
```

Admin UI:

```text
http://localhost:5000/admin
```

The admin templates and i18n resources are embedded into the `regimuxd` binary and rendered through Fiber's template engine. The UI uses Tailwind CSS and htmx from CDN, supports `?lang=zh` / `?lang=en`, and follows the browser's light/dark color-scheme preference.

Admin pages currently include dashboard, upstream health, pull/activity records, cache and storage summaries, scheduler config, manual sync, auth audit, and effective config. Manual sync can warm a configured upstream image such as `gitlab/gitlab-ce:latest` or `library/node:20` through the existing manifest/blob cache path. When `auth.enabled = true`, `/admin` is protected with HTTP Basic using the same configured users as Docker Registry auth.

配置文件示例：
- 最小化：`configs/regimux.minimal.hcl`（只覆盖启动监听和 `hub` 上游）
- 完整：`configs/regimux.full.hcl`（包含所有可配置项，适合直接复用）

命令行覆盖配置（由 configx 解析）：

```bash
go run ./cmd/regimuxd --config configs/regimux.minimal.hcl --server.listen=:6000 --worker.prefetch_concurrency=4
```

环境变量和 dotenv 覆盖配置：

RegiMux 使用 `configx` 从 `.env`、`.env.local`、HCL 文件、环境变量和命令行参数加载配置。`REGIMUX_*` 环境变量和 dotenv 中的同名变量会覆盖 HCL 文件，命令行参数会继续覆盖环境变量。环境变量前缀是 `REGIMUX_`，嵌套路径使用双下划线 `__`：

```bash
REGIMUX_SERVER__LISTEN=:6000
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
REGIMUX_UPSTREAMS__HUB__REGISTRY=https://registry-1.docker.io
```

因此配置文件中的 `cache.redis.addrs` 可以用 `REGIMUX_CACHE__REDIS__ADDRS` 覆盖，`upstreams.hub.registry` 可以用 `REGIMUX_UPSTREAMS__HUB__REGISTRY` 覆盖。

S3-compatible object storage:

```hcl
store {
  object {
    driver = "s3"

    s3 {
      bucket = "regimux-objects"
      prefix = "cache"
      region = "us-east-1"
      endpoint = "http://minio:9000"
      access_key_id = "regimux"
      secret_access_key = "change-me"
      force_path_style = true
    }
  }
}
```

The S3 driver uses `github.com/fclairamb/afero-s3` under `afero.NewBasePathFs`, so `prefix` behaves like the object store root. AWS S3, MinIO, R2, and other S3-compatible services can be configured through the same block.

SFTP object storage:

```hcl
store {
  object {
    driver = "sftp"
    path = "/srv/regimux/objects"

    sftp {
      addr = "sftp.example.com:22"
      username = "regimux"
      password = "change-me"
      known_hosts_path = "/etc/regimux/known_hosts"
      timeout = "10s"
    }
  }
}
```

The SFTP driver uses the official `github.com/spf13/afero/sftpfs` module under `afero.NewBasePathFs`. Host key verification is required through either `known_hosts_path` or a pinned `host_key`.

认证：

RegiMux 默认不启用入口认证。开启后会使用 Docker Registry Bearer token flow，支持 `docker login`，账号和密码可以放在配置文件中：

```hcl
auth {
  enabled = true
  service = "regimux"
  token_secret = "change-me"
  token_ttl = "15m"

  users {
    alice {
      password_hash = "$2a$12$replace-with-bcrypt-hash"
      repositories = ["hub/*", "ghcr/my-org/*"]
    }
  }
}
```

开发环境也可以使用 `password = "plain-text"`，生产配置建议只使用 `password_hash`。`repositories` 使用 RegiMux 路径空间，例如 `hub/library/alpine` 或 `hub/*`。

发布：

- 推送 `v*` tag 会触发 `.github/workflows/release.yml`。
- 发布前会先执行 `go test ./...`、`golangci-lint run ./...` 和 `goreleaser check`。
- GoReleaser 会产出 Linux / macOS / Windows 的 `amd64`、`arm64` 归档；Windows zip 内包含 `regimuxd.exe`，同时会直接上传 Windows `.exe`。
- GoReleaser 会产出 Linux `amd64`、`arm64` 的 `.deb` 和 `.rpm` 包，默认安装配置到 `/etc/regimux/regimux.hcl`。
- GoReleaser 会自动创建 GitHub Release，并上传归档和 checksum。
- Docker 镜像发布到 `ghcr.io/<owner>/<repo>`，Alpine 镜像使用默认标签 `latest`，Debian 镜像使用 `latest-debian` / `debian`。
- CI 会安装 UPX，Linux 二进制会在进入 Docker 镜像前压缩。

Docker Compose 示例：

- `examples/compose/compose.memory.yml`：单机内存缓存。
- `examples/compose/compose.redis.yml`：Redis 缓存和分布式调度锁。
- `examples/compose/compose.valkey.yml`：Valkey 缓存和分布式调度锁。
- `examples/compose/compose.observability.yml`：RegiMux + Prometheus。

Compose 示例会读取 `examples/compose/.env.example` 和可选的 `examples/compose/.env`，可以直接用 `REGIMUX_*` 环境变量覆盖容器内配置。

详见 `examples/compose/README.md`。

Roadmap 见 `ROADMAP.md`。
