# 配置

RegiMux 使用 `configx` 加载强类型 HCL 配置。发布包和容器镜像会提供默认配置 `/etc/regimux/regimux.hcl`。

配置围绕依赖代理 namespace 组织。每个顶层生态块定义一个或多个 alias；每个 alias 会变成客户端可访问的代理前缀，并指向一个上游以及可选的 mirror、认证、探测和 HTTP 策略。

## 配置文件

仓库内示例：

- [configs/regimux.minimal.hcl](../../configs/regimux.minimal.hcl)：最小可运行配置。
- [configs/regimux.hcl](../../configs/regimux.hcl)：常用默认配置。
- [configs/regimux.full.hcl](../../configs/regimux.full.hcl)：完整配置参考。

最小配置：

```hcl
server {
  listen = ":8080"
}

container {
  hub {
    registry = "https://registry-1.docker.io"
  }
}

go {
  default {
    registry = "https://proxy.golang.org"
  }
}

npm {
  default {
    registry = "https://registry.npmjs.org"
  }
}

pypi {
  default {
    registry = "https://pypi.org"
  }
}

maven {
  central {
    registry = "https://repo.maven.apache.org/maven2"
  }
}

dist {
  gradle {
    registry = "https://services.gradle.org/distributions"
    mirrors = ["https://dist-cache.example.com/gradle"]
    mirror_policy = "ordered"
    allow = ["gradle-*-bin.zip", "gradle-*-all.zip"]
  }
}
```

## 默认值

默认值以 Go 强类型配置定义，然后在合并配置文件、dotenv、环境变量和命令行来源后统一校验。

关键默认值：

- `server.listen = ":8080"`
- `server.public_url = "http://localhost:8080"`
- `server.middleware.request_id.enabled = true`
- `server.middleware.request_logger.enabled = false`
- `server.middleware.healthcheck.enabled = true`，提供 `/livez` 和 `/readyz`
- `server.middleware.etag.enabled = true`，但跳过 registry `/v2` 流量
- `server.middleware.compress.enabled = true`，但跳过 registry `/v2` 流量
- `server.middleware.security_headers.enabled = true`，但跳过 registry `/v2` 流量
- `server.middleware.security_headers.cross_origin_embedder_policy = "unsafe-none"`，让内置 admin UI 可以加载 CDN 静态资源
- `server.middleware.rate_limit.enabled = false`
- `server.middleware.csrf.enabled = false`
- `server.middleware.pprof.enabled = false`
- `cache.backend = ""`（KV 缓存默认不启用；需要时显式设置 `memory`、`redis` 或 `valkey`）
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
- `scheduler.refresh.enabled = true`
- `scheduler.refresh.window = "10m"`，按制品对近期 pull 触发的刷新意图做去重后再调度上游刷新
- `scheduler.refresh.distributed = true`
- `scheduler.manifest_refresh.enabled = false`
- `scheduler.manifest_refresh.ecosystems` 可按生态覆盖 manifest refresh（`container`、`go`、`npm`、`pypi`、`maven`）
- `docker.enabled = false`
- `docker.observe = true`
- `docker.prewarm.alias = "hub"`
- `docker.prewarm.timeout = "10m"`
- `container.hub.registry = "https://registry-1.docker.io"`
- `container.ghcr.registry = "https://ghcr.io"`
- `container.quay.registry = "https://quay.io"`
- `container.hub.http.http2.enabled = false`
- `go.default.registry = "https://proxy.golang.org"`
- `npm.default.registry = "https://registry.npmjs.org"`
- `pypi.default.registry = "https://pypi.org"`
- `maven.central.registry = "https://repo.maven.apache.org/maven2"`
- `dist.gradle.registry = "https://services.gradle.org/distributions"`
- `dist.gradle.allow = ["gradle-*-bin.zip", "gradle-*-all.zip"]`

顶层生态块定义依赖代理 namespace：

- `container`：OCI / Docker Registry V2 依赖代理 alias，通过 `/v2/{containerAlias}/...` 暴露。
- `go`：Go module 依赖代理 alias，通过 `/go/{goAlias}/...` 暴露。
- `npm`：npm 依赖代理 alias，通过 `/npm/{npmAlias}/...` 暴露。
- `pypi`：PyPI 依赖代理 alias，通过 `/pypi/{pypiAlias}/...` 暴露。
- `maven`：Maven 依赖代理 alias，通过 `/maven/{mavenAlias}/...` 暴露。
- `dist`：二进制分发物 mirror alias，通过 `/dist/{distAlias}/...` 暴露。

这些块也是生态 runtime 层的输入。RegiMux 会把它们归一化为带生态类型、alias、registry、mirrors、probe 设置、auth 和 HTTP 策略的 runtime 条目。调度器随后从 `probe`、`prefetch` 等 runtime capability 工作，而不是读取 legacy `upstreams` 块。

`dist` alias 支持和其他依赖代理生态一致的 `registry`、`mirrors`、`mirror_policy`、`probe`、`auth`、`http` 字段，并额外使用 `allow` 路径模式限制可代理的二进制分发物。这是一套通用文件下载 mirror：Gradle distribution、Electron release artifact、CLI installer 或内部二进制发布都可以按需增加多个 alias。请求会按健康排序后的 registry/mirror endpoint 依次尝试；传输错误以及上游 `404`、`410`、`408`、`429` 或 `5xx` 响应会在还有后续 endpoint 时继续切换，`403` 和其他不可重试响应会直接返回给客户端。

container runtime 暴露定时 `probe` 和预测性 `prefetch` capability。Go、npm、PyPI、Maven 和 dist 在 alias 配置 `probe.enabled = true` 后会暴露通用 endpoint `probe` capability，同时也会参与定时 `prefetch`，用于刷新近期请求过的制品。

RegiMux 默认会关闭上游 registry 客户端的 HTTP/2。这样可以让 mirror 和 CDN 链路更可控，并避免 HTTP/2 运行时 panic 直接打崩进程。只建议对可信上游按 alias 显式开启：

```hcl
container {
  hub {
    http {
      http2 {
        enabled = true
      }
    }
  }
}
```

`cache.blob.stream_and_cache` 默认开启。完整 blob miss 会边回传给 Docker 边写入对象存储；只有客户端完整读完 blob 流后，才会提交缓存和元数据。HEAD 请求不会存储 blob 字节；range miss 会先补齐完整 blob 到对象存储，再从本地对象返回 range。

Admin 的 `已落盘 Blob 字节（metadata）` 来自已提交的 blob metadata，不是实时扫描 `store.object`，也不是所有经过代理的字节。`cache.backend` 是 KV 缓存后端，和 `store.object` 配置的对象存储不是同一层。

## 依赖策略

`policy.dependency` 用于在发起上游前对依赖请求做前置控制，避免不必要的流量打到上游。

- `dependency.block` 优先于 `dependency.allow`，先执行。
- `dependency.allow` 不为空时，请求必须命中至少一条 allow 规则；否则拒绝。
- `dependency.allow` 为空时默认放行，但命中 block 的请求仍会拒绝。
- 规则字段会去掉首尾空白，生态名会做大小写归一化。
- 生态名、alias、artifact、reference 可精确匹配；字段值以 `*` 结尾时做前缀通配。
- 字段为空表示“任意（通配）”。

不同生态对应字段如下：

- `ecosystem`：`container`、`go`、`npm`、`pypi`、`maven`
- `alias`：请求路径中的上游 alias
- `artifact`：
  - container：仓库名，例如 `library/alpine`
  - go：模块路径，例如 `github.com/example/mod`
  - npm：包名
  - pypi：`pypi/simple/<project>`
  - maven：artifact 目录路径
- `reference`：
  - container：按路由类型可能是 tag、digest 或 `tags`
  - go：请求 ref，例如 `@v/v1.2.3.zip`
  - npm：`metadata`、`tarball:...`、`path:...`
  - pypi：`index.html` 或标准化后的 package path
  - maven：文件名

被拒绝的请求返回 `403 Forbidden`，且不会访问上游。它们仍会写入 pull metadata 的策略拒绝计数和最近拒绝时间，admin 可以看到被拦截的依赖代理流量，但这不会计入成功拉取。Prometheus 会通过 `regimux_service_dependency_proxy_pulls_total{ecosystem,kind,alias,repository,status}` 暴露依赖代理 pull 结果，并通过 `regimux_service_dependency_proxy_policy_denied_pulls_total{ecosystem,kind,alias,repository}` 暴露策略拒绝的 pull。

```hcl
policy {
  dependency {
    allow {
      ecosystem = "go"
      alias = "default"
      artifact = "github.com/example/*"
      reference = "v1.2.3"
    }

    block {
      ecosystem = "container"
      alias = "hub"
      artifact = "private/*"
      reference = "*"
    }
  }
}
```

small blob cache 可以把已经完成 digest 校验的小 blob，例如 OCI image config blob，放进当前配置的 KV 缓存后端。这个模式建议搭配 Redis 或 Valkey 使用；大 layer 仍然应该放在 `store.object`。

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

## Docker Daemon 集成

`docker` 块是可选能力，默认关闭。开启后 RegiMux 会通过 Docker socket 连接宿主 Docker daemon，可监听本机 image 事件，也可以在启动后让宿主 Docker 通过 RegiMux 代理拉取一组镜像，从而预热缓存。

容器运行时需要显式挂载 socket，例如 Linux Docker Engine 下挂载 `/var/run/docker.sock:/var/run/docker.sock`。Docker Desktop 场景里，`prewarm.registry` 应填写 Docker daemon 能访问的地址，例如 `192.168.1.2:8080`，而不是容器内部的 `localhost:8080`。

```hcl
docker {
  enabled = true
  observe = true

  prewarm {
    enabled = true
    registry = "192.168.1.2:8080"
    alias = "hub"
    images = ["alpine:latest", "library/nginx:1.27"]
    timeout = "10m"
  }
}
```

## 环境变量

环境变量使用 `REGIMUX_` 前缀，并用 `__` 表示嵌套：

```text
REGIMUX_SERVER__LISTEN=:8080
REGIMUX_SERVER__PUBLIC_URL=http://localhost:8080
REGIMUX_LOG__LEVEL=debug
REGIMUX_LOG__DEBUG=true
REGIMUX_SERVER__MIDDLEWARE__REQUEST_LOGGER__ENABLED=true
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
REGIMUX_CACHE__BLOB__SMALL_CACHE__ENABLED=true
REGIMUX_DOCKER__ENABLED=true
REGIMUX_DOCKER__PREWARM__REGISTRY=192.168.1.2:8080
REGIMUX_SCHEDULER__MANIFEST_REFRESH__ECOSYSTEMS__CONTAINER__ENABLED=true
REGIMUX_SCHEDULER__MANIFEST_REFRESH__ECOSYSTEMS__CONTAINER__INTERVAL=10m
REGIMUX_CONTAINER__HUB__REGISTRY=https://registry-1.docker.io
REGIMUX_CONTAINER__HUB__HTTP__HTTP2__ENABLED=true
REGIMUX_GO__DEFAULT__REGISTRY=https://proxy.golang.org
REGIMUX_NPM__DEFAULT__REGISTRY=https://registry.npmjs.org
REGIMUX_PYPI__DEFAULT__REGISTRY=https://pypi.org
REGIMUX_MAVEN__CENTRAL__REGISTRY=https://repo.maven.apache.org/maven2
REGIMUX_DIST__GRADLE__REGISTRY=https://services.gradle.org/distributions
```

加载器会在存在 `.env` 时读取它。环境变量优先级高于 `.env` 和配置文件。

## 命令行覆盖

Cobra 未识别的 flag 会传给 `configx` 作为配置覆盖：

```bash
regimuxd --config /etc/regimux/regimux.hcl --server.listen=:8080 --log.level=debug
```

`log.debug = true` 和 `REGIMUX_LOG__DEBUG=true` 被保留作为兼容写法，会等价为 debug 日志级别；推荐优先使用 `log.level = "debug"`。

小范围运行时覆盖可以用命令行；较完整的配置建议放在 HCL 或环境变量里。

## 校验

配置校验会拒绝无效枚举值、无效 URL、负数时长或数量、无效清理水位、不支持的存储驱动、不完整的 S3/SFTP 凭证，以及指向不存在 container alias 的 Docker 预热配置。

支持的元数据驱动：

- `sqlite`
- `mysql`
- `postgres`
- `postgresql`（`postgres` 的等价写法）

支持的对象存储驱动：

- `local`
- `memory`
- `s3`
- `sftp`
