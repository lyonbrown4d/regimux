# 配置

RegiMux 使用 `configx` 加载强类型 HCL 配置。发布包和容器镜像会提供默认配置 `/etc/regimux/regimux.hcl`。

## 配置文件

仓库内示例：

- [configs/regimux.minimal.hcl](../../configs/regimux.minimal.hcl)：最小可运行配置。
- [configs/regimux.hcl](../../configs/regimux.hcl)：常用默认配置。
- [configs/regimux.full.hcl](../../configs/regimux.full.hcl)：完整配置参考。

最小配置：

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

## 默认值

默认值以 Go 强类型配置定义，然后在合并配置文件、dotenv、环境变量和命令行来源后统一校验。

关键默认值：

- `server.listen = ":5000"`
- `server.public_url = "http://localhost:5000"`
- `server.middleware.request_id.enabled = true`
- `server.middleware.healthcheck.enabled = true`，提供 `/livez` 和 `/readyz`
- `server.middleware.etag.enabled = true`，但跳过 registry `/v2` 流量
- `server.middleware.compress.enabled = true`，但跳过 registry `/v2` 流量
- `server.middleware.security_headers.enabled = true`，但跳过 registry `/v2` 流量
- `server.middleware.security_headers.cross_origin_embedder_policy = "unsafe-none"`，让内置 admin UI 可以加载 CDN 静态资源
- `server.middleware.rate_limit.enabled = false`
- `server.middleware.csrf.enabled = false`
- `server.middleware.pprof.enabled = false`
- `cache.backend = "memory"`
- `cache.blob.small_cache.enabled = false`
- `cache.blob.small_cache.max_size_bytes = 4194304`
- `cache.blob.small_cache.ttl = "24h"`
- `store.meta.driver = "sqlite"`
- `store.meta.path = "data/regimux.db"`
- `store.object.driver = "local"`
- `store.object.path = "data/objects"`
- `scheduler.cleanup.enabled = true`
- `scheduler.prefetch.enabled = false`
- `docker.enabled = false`
- `docker.observe = true`
- `docker.prewarm.alias = "hub"`
- `docker.prewarm.timeout = "10m"`
- `upstreams.hub.registry = "https://registry-1.docker.io"`
- `upstreams.hub.http.http2.enabled = false`

RegiMux 默认会关闭上游 registry 客户端的 HTTP/2。这样可以让 mirror 和 CDN 链路更可控，并避免 HTTP/2 运行时 panic 直接打崩进程。只建议对可信上游按 alias 显式开启：

```hcl
upstreams {
  hub {
    http {
      http2 {
        enabled = true
      }
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

容器运行时需要显式挂载 socket，例如 Linux Docker Engine 下挂载 `/var/run/docker.sock:/var/run/docker.sock`。Docker Desktop 场景里，`prewarm.registry` 应填写 Docker daemon 能访问的地址，例如 `192.168.1.2:5000`，而不是容器内部的 `localhost:5000`。

```hcl
docker {
  enabled = true
  observe = true

  prewarm {
    enabled = true
    registry = "192.168.1.2:5000"
    alias = "hub"
    images = ["alpine:latest", "library/nginx:1.27"]
    timeout = "10m"
  }
}
```

## 环境变量

环境变量使用 `REGIMUX_` 前缀，并用 `__` 表示嵌套：

```text
REGIMUX_SERVER__LISTEN=:5000
REGIMUX_SERVER__PUBLIC_URL=http://localhost:5000
REGIMUX_LOG__LEVEL=debug
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
REGIMUX_CACHE__BLOB__SMALL_CACHE__ENABLED=true
REGIMUX_DOCKER__ENABLED=true
REGIMUX_DOCKER__PREWARM__REGISTRY=192.168.1.2:5000
REGIMUX_UPSTREAMS__HUB__REGISTRY=https://registry-1.docker.io
REGIMUX_UPSTREAMS__HUB__HTTP__HTTP2__ENABLED=true
```

加载器会在存在 `.env` 时读取它。环境变量优先级高于 `.env` 和配置文件。

## 命令行覆盖

Cobra 未识别的 flag 会传给 `configx` 作为配置覆盖：

```bash
regimuxd --config /etc/regimux/regimux.hcl --server.listen=:5000 --log.level=debug
```

小范围运行时覆盖可以用命令行；较完整的配置建议放在 HCL 或环境变量里。

## 校验

配置校验会拒绝无效枚举值、无效 URL、负数时长或数量、无效清理水位、不支持的存储驱动、不完整的 S3/SFTP 凭证，以及指向不存在上游 alias 的 Docker 预热配置。

支持的元数据驱动：

- `sqlite`
- `mysql`
- `postgres`

支持的对象存储驱动：

- `local`
- `memory`
- `s3`
- `sftp`
