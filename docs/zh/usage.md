# 使用指南

RegiMux 默认通过 [GitHub Releases](https://github.com/lyonbrown4d/regimux/releases) 和 GitHub Container Registry 发布。推荐优先使用发布产物，而不是直接从源码运行。

## Docker

默认镜像基于 Alpine：

```bash
docker run --rm \
  -p 5000:5000 \
  -v regimux-data:/var/lib/regimux \
  ghcr.io/lyonbrown4d/regimux:latest
```

也可以固定版本：

```bash
docker run --rm \
  -p 5000:5000 \
  -v regimux-data:/var/lib/regimux \
  ghcr.io/lyonbrown4d/regimux:v0.0.2
```

Debian 镜像使用 `-debian` 后缀：

```bash
docker run --rm \
  -p 5000:5000 \
  -v regimux-data:/var/lib/regimux \
  ghcr.io/lyonbrown4d/regimux:v0.0.2-debian
```

容器默认读取 `/etc/regimux/regimux.hcl`，工作目录为 `/var/lib/regimux`，监听 `:5000`。

## deb 和 rpm

从 [GitHub Release 页面](https://github.com/lyonbrown4d/regimux/releases) 下载匹配系统架构的包：

```bash
sudo dpkg -i ./regimuxd_*_linux_amd64.deb
sudo systemctl enable --now regimuxd
```

rpm 系统：

```bash
sudo rpm -Uvh ./regimuxd_*_linux_amd64.rpm
sudo systemctl enable --now regimuxd
```

包会安装：

- `/usr/bin/regimuxd`
- `/etc/regimux/regimux.hcl`
- `/lib/systemd/system/regimuxd.service`
- `/var/lib/regimux`

## 压缩包和 Windows exe

GitHub Releases 包含 Linux/macOS 的 tar 包、Windows zip 包，以及独立的 Windows 二进制产物。

Linux 示例：

```bash
tar -xzf regimux_0.0.2_linux_amd64.tar.gz
./regimuxd --config configs/regimux.minimal.hcl
```

Windows 示例：

```powershell
Expand-Archive .\regimux_0.0.2_windows_amd64.zip
.\regimux_0.0.2_windows_amd64\regimuxd.exe --config .\regimux_0.0.2_windows_amd64\configs\regimux.minimal.hcl
```

## 健康检查

```bash
curl -i http://localhost:5000/healthz
curl -i http://localhost:5000/v2/
```

## 拉取镜像

RegiMux 使用仓库路径的第一段作为上游别名：

```text
localhost:5000/hub/library/alpine:latest
localhost:5000/ghcr/org/app:v1.2.3
localhost:5000/quay/coreos/etcd:v3.5.0
```

通过 Docker 拉取：

```bash
docker pull localhost:5000/hub/library/alpine:latest
```

直接请求 manifest：

```bash
curl -i \
  -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
  http://localhost:5000/v2/hub/library/alpine/manifests/latest
```

## Go Module Proxy

默认配置包含 `golang` upstream，指向 `https://proxy.golang.org`。Go 客户端可以把 RegiMux 当作 Go module proxy：

```bash
export GOPROXY=http://localhost:5000,direct
go env GOPROXY
go mod download github.com/pkg/errors@v0.9.1
```

RegiMux 会代理并缓存 Go proxy 协议请求，例如：

```text
GET /github.com/pkg/errors/@v/list
GET /github.com/pkg/errors/@v/v0.9.1.info
GET /github.com/pkg/errors/@v/v0.9.1.mod
GET /github.com/pkg/errors/@v/v0.9.1.zip
GET /github.com/pkg/errors/@latest
```

Root Go proxy 请求会按稳定的 alias 顺序降级尝试所有 `type = "go"` upstream，并优先使用 `golang` alias。需要显式指定某个 Go upstream 时，兼容路径 `/go/{alias}/...` 仍然可用。

`@latest` 和 `@v/list` 使用短 TTL；版本化的 `.info`、`.mod` 和 `.zip` 按内容 sha256 写入对象存储并长期复用。当前实现是只读 read-through proxy，不代理 `sum.golang.org`，也不做 VCS direct 拉取。

## Docker Compose

Compose 示例默认使用已经发布到 GHCR 的镜像：

```bash
cp examples/compose/.env.example examples/compose/.env
docker compose --env-file examples/compose/.env -f examples/compose/compose.memory.yml up -d
```

其他示例：

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.redis.yml up -d
docker compose --env-file examples/compose/.env -f examples/compose/compose.valkey.yml up -d
docker compose --env-file examples/compose/.env -f examples/compose/compose.observability.yml up -d
```

更多说明见：[Compose 示例](../../examples/compose/README.md)。

## Admin UI

访问：

```text
http://localhost:5000/admin
```

Admin UI 已内嵌到二进制中，包含仪表盘、上游健康、拉取记录、活动、缓存、存储、调度器、手动同步、认证审计和有效配置等页面。

手动同步会通过现有 manifest 和 blob 缓存流程预热镜像，例如：

```text
hub/library/node:20
hub/gitlab/gitlab-ce:latest
```

当 `auth.enabled = true` 时，`/admin` 会使用同一套配置用户做 HTTP Basic 认证。

## 配置覆盖

RegiMux 通过 `configx` 读取强类型 HCL 配置、dotenv、环境变量和命令行覆盖。

环境变量使用 `REGIMUX_` 前缀，并用 `__` 表示嵌套：

```text
REGIMUX_SERVER__PUBLIC_URL=http://localhost:5000
REGIMUX_LOG__LEVEL=debug
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
```

命令行覆盖使用点分隔 key：

```bash
regimuxd --config /etc/regimux/regimux.hcl --server.listen=:5000 --log.level=debug
```

## 开发运行

从源码开发时可以使用：

```bash
go run ./cmd/regimuxd --config configs/regimux.minimal.hcl
```

常见下一步：

- 修改运行配置：[配置](configuration.md)。
- 开启认证和 `docker login`：[认证](auth.md)。
- 使用 Redis、Valkey、S3、SFTP、MySQL 或 PostgreSQL：[存储](storage.md)。
- 调整清理和预拉取策略：[调度器](scheduler.md)。
