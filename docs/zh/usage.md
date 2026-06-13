# 使用指南

RegiMux 默认通过 [GitHub Releases](https://github.com/lyonbrown4d/regimux/releases) 和 GitHub Container Registry 发布。推荐优先使用发布产物，而不是直接从源码运行。

## Dependency Proxy（依赖代理）模型

把 RegiMux 部署在开发者、CI runner 或构建集群附近，然后让依赖客户端访问 RegiMux，而不是直接访问每个公网源：

- Docker/containerd 使用兼容 Registry 的 `/v2/{containerAlias}/...` 路径。
- Go 使用 `GOPROXY=http://<regimux>/go/{goAlias}`。
- npm、PyPI 和 Maven 分别使用 `/npm/{npmAlias}`、`/pypi/{pypiAlias}`、`/maven/{mavenAlias}` 下的生态代理路径。
- Gradle wrapper zip、CLI installer 等二进制分发物使用 `/dist/{distAlias}/{path}`。

RegiMux 是只读的。它代理依赖读取请求，缓存不可变制品，维护 metadata 用于缓存统计和清理，并可以在后台预热或刷新制品。它不是包发布入口，也不是 push registry。

## Docker

默认镜像基于 Alpine：

```bash
docker run --rm \
  -p 8080:8080 \
  -v regimux-data:/var/lib/regimux \
  ghcr.io/lyonbrown4d/regimux:latest
```

也可以固定版本：

```bash
docker run --rm \
  -p 8080:8080 \
  -v regimux-data:/var/lib/regimux \
  ghcr.io/lyonbrown4d/regimux:v0.0.2
```

Debian 镜像使用 `-debian` 后缀：

```bash
docker run --rm \
  -p 8080:8080 \
  -v regimux-data:/var/lib/regimux \
  ghcr.io/lyonbrown4d/regimux:v0.0.2-debian
```

容器默认读取 `/etc/regimux/regimux.hcl`，工作目录为 `/var/lib/regimux`，监听 `:8080`。

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
curl -i http://localhost:8080/livez
curl -i http://localhost:8080/readyz
curl -i http://localhost:8080/v2/
```

## 容器镜像

RegiMux 使用仓库路径的第一段作为 container alias：

```text
localhost:8080/{containerAlias}/library/alpine:latest
localhost:8080/{containerAlias}/org/app:v1.2.3
localhost:8080/{containerAlias}/coreos/etcd:v3.5.0
```

通过 container 依赖代理拉取镜像：

```bash
docker pull localhost:8080/{containerAlias}/library/alpine:latest
```

直接请求 manifest：

```bash
curl -i \
  -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
  http://localhost:8080/v2/{containerAlias}/library/alpine/manifests/latest
```

## Go Module Proxy

`go` 配置块下的每个 alias 都会在 `/go/{goAlias}` 暴露 Go module proxy 入口。Go 客户端通过 `GOPROXY` 把 RegiMux 作为依赖代理：

```bash
export GOPROXY=http://localhost:8080/go/{goAlias},direct
go env GOPROXY
go mod download github.com/pkg/errors@v0.9.1
```

RegiMux 会代理并缓存 Go proxy 协议请求，例如：

```text
GET /go/{goAlias}/github.com/pkg/errors/@v/list
GET /go/{goAlias}/github.com/pkg/errors/@v/v0.9.1.info
GET /go/{goAlias}/github.com/pkg/errors/@v/v0.9.1.mod
GET /go/{goAlias}/github.com/pkg/errors/@v/v0.9.1.zip
GET /go/{goAlias}/github.com/pkg/errors/@latest
```

选中的 Go alias 只在 `go` 配置块内解析。container、npm、PyPI 和 Maven alias 各自使用独立命名空间。

`@latest` 和 `@v/list` 使用短 TTL；版本化的 `.info`、`.mod` 和 `.zip` 按内容 sha256 写入对象存储并长期复用。当前实现是只读 Go dependency proxy，不代理 `sum.golang.org`，也不做 VCS direct 拉取。

## Dist Mirror

`dist` 配置块下的每个 alias 都会在 `/dist/{distAlias}/{path}` 暴露一个通用文件下载 mirror。RegiMux 不需要为 Gradle、Electron、CLI installer 或其他 release asset 写特定逻辑；它们都只是用户配置出来的 alias，包含上游、可选 mirrors 和路径 allow 规则。默认 `gradle` alias 指向 `https://services.gradle.org/distributions`，并允许 Gradle wrapper 压缩包：

```text
GET /dist/gradle/gradle-8.7-bin.zip
GET /dist/gradle/gradle-8.7-all.zip
```

有内网缓存或区域分发源时，可以为 dist alias 配置额外 mirror：

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

在 `gradle/wrapper/gradle-wrapper.properties` 中可以这样使用：

```properties
distributionUrl=http\://localhost\:8080/dist/gradle/gradle-8.7-bin.zip
```

Electron 通过 npm 安装时，npm 先从 npm registry 下载 `electron` 包，然后 Electron 的安装脚本使用 `@electron/get` 下载 release artifact。`@electron/get` 会用 mirror base、版本目录和文件名拼出下载 URL，并且会读取 `.npmrc`、package config 或 `ELECTRON_MIRROR` 等环境变量。把这个 mirror base 指向用户配置的 dist alias 即可：

```ini
electron_mirror=http://localhost:8080/dist/electron/
```

或：

```bash
export ELECTRON_MIRROR=http://localhost:8080/dist/electron/
npm install electron
```

其他常见客户端也有类似的下载源配置：

```bash
PLAYWRIGHT_DOWNLOAD_HOST=http://localhost:8080/dist/playwright npx playwright install
CYPRESS_DOWNLOAD_MIRROR=http://localhost:8080/dist/cypress cypress install
npm_config_disturl=http://localhost:8080/dist/nodejs npm rebuild
```

没有内置 mirror 配置的 release 站点，可以在 CI 脚本里直接使用 dist URL，例如：

```bash
curl -LO http://localhost:8080/dist/hashicorp/terraform/1.9.0/terraform_1.9.0_linux_amd64.zip
```

完整 `GET` 响应会按内容 sha256 写入对象存储，并写入 metadata 用于缓存统计和清理。`HEAD` 请求不会存储字节。已缓存完整对象时，`Range` 请求会从本地对象切片返回；未命中的 `Range` 请求只透传上游，不会把 partial 内容作为对象落盘。

当 dist endpoint 出现传输错误，或返回 `404`、`410`、`408`、`429`、`5xx` 时，如果还有后续 mirror，RegiMux 会继续尝试下一个 endpoint。`403` 等不可重试响应会直接返回给客户端。

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
http://localhost:8080/admin
```

Admin UI 已内嵌到二进制中，包含仪表盘、上游健康、拉取记录、活动、缓存、存储、调度器、手动刷新、认证审计和有效配置等页面。

手动刷新支持生态隔离（`container`、`go`、`npm`、`pypi`、`maven`、`dist`），并作为后台任务执行：

```text
container:hub / repository=library/node / reference=20
go:default / repository=github.com/pkg/errors / reference=v0.9.1
dist:gradle / repository=dist / reference=gradle-8.7-bin.zip
```

当 `auth.enabled = true` 时，`/admin` 会使用同一套配置用户做 HTTP Basic 认证。

## 配置覆盖

RegiMux 通过 `configx` 读取强类型 HCL 配置、dotenv、环境变量和命令行覆盖。

环境变量使用 `REGIMUX_` 前缀，并用 `__` 表示嵌套：

```text
REGIMUX_SERVER__PUBLIC_URL=http://localhost:8080
REGIMUX_LOG__LEVEL=debug
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
```

命令行覆盖使用点分隔 key：

```bash
regimuxd --config /etc/regimux/regimux.hcl --server.listen=:8080 --log.level=debug
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
