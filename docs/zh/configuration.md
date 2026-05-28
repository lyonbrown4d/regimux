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
- `cache.backend = "memory"`
- `store.meta.driver = "sqlite"`
- `store.meta.path = "data/regimux.db"`
- `store.object.driver = "local"`
- `store.object.path = "data/objects"`
- `scheduler.cleanup.enabled = true`
- `scheduler.prefetch.enabled = false`
- `upstreams.hub.registry = "https://registry-1.docker.io"`

## 环境变量

环境变量使用 `REGIMUX_` 前缀，并用 `__` 表示嵌套：

```text
REGIMUX_SERVER__LISTEN=:5000
REGIMUX_SERVER__PUBLIC_URL=http://localhost:5000
REGIMUX_LOG__LEVEL=debug
REGIMUX_CACHE__BACKEND=redis
REGIMUX_CACHE__REDIS__ADDRS=redis:6379
REGIMUX_UPSTREAMS__HUB__REGISTRY=https://registry-1.docker.io
```

加载器会在存在 `.env` 时读取它。环境变量优先级高于 `.env` 和配置文件。

## 命令行覆盖

Cobra 未识别的 flag 会传给 `configx` 作为配置覆盖：

```bash
regimuxd --config /etc/regimux/regimux.hcl --server.listen=:5000 --log.level=debug
```

小范围运行时覆盖可以用命令行；较完整的配置建议放在 HCL 或环境变量里。

## 校验

配置校验会拒绝无效枚举值、无效 URL、负数时长或数量、无效清理水位、不支持的存储驱动，以及不完整的 S3/SFTP 凭证。

支持的元数据驱动：

- `sqlite`
- `mysql`
- `postgres`

支持的对象存储驱动：

- `local`
- `memory`
- `s3`
- `sftp`

