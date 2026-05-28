# Docker Compose

Compose 示例位于 [examples/compose](../../examples/compose)。它们默认使用已经发布到 GHCR 的镜像：

```text
ghcr.io/lyonbrown4d/regimux:latest
```

需要固定版本、修改端口或覆盖运行配置时，先复制环境变量模板：

```bash
cp examples/compose/.env.example examples/compose/.env
```

## 示例

单节点 memory cache：

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.memory.yml up -d
```

Redis cache 和分布式调度锁：

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.redis.yml up -d
```

Valkey cache 和分布式调度锁：

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.valkey.yml up -d
```

Prometheus 抓取：

```bash
docker compose --env-file examples/compose/.env -f examples/compose/compose.observability.yml up -d
```

## 固定版本

在 `examples/compose/.env` 中设置：

```text
REGIMUX_IMAGE=ghcr.io/lyonbrown4d/regimux:v0.0.2
REGIMUX_HTTP_PORT=5000
```

Debian 镜像：

```text
REGIMUX_IMAGE=ghcr.io/lyonbrown4d/regimux:v0.0.2-debian
```

## 运行时覆盖

Compose 会把 `REGIMUX_*` 变量传入容器。RegiMux 通过 `configx` 加载这些变量：

```text
REGIMUX_SERVER__PUBLIC_URL=http://localhost:5000
REGIMUX_LOG__LEVEL=debug
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
```

更细的示例说明见 [examples/compose/README.md](../../examples/compose/README.md)。

