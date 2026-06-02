# 设计

## 定位

RegiMux 是一个只读研发依赖缓存网关。当前稳定能力包括 OCI / Docker Registry V2 代理镜像和 Go module proxy read-through cache；后续会继续接入 Maven、PyPI 和 npm。

OCI 部分对外暴露兼容 Registry 的 pull API，并通过配置的 alias 将请求路由到不同上游 registry。Go 部分在根路径暴露 Go module proxy 协议，并将请求路由到配置为 `type = "go"` 的上游；兼容路径 `/go/{alias}/...` 仍可用于显式选择某个 Go upstream。

RegiMux 不是 push registry。上传、manifest 写入和删除 API 当前都不在范围内。

## OCI 请求模型

镜像名使用仓库路径的第一段作为上游 alias：

```text
localhost:5000/hub/library/alpine:latest
localhost:5000/ghcr/org/app:v1.2.3
```

Registry API 示例：

```text
GET /v2/hub/library/alpine/manifests/latest
GET /v2/hub/library/alpine/blobs/sha256:...
GET /v2/hub/library/alpine/tags/list
GET /v2/hub/library/alpine/referrers/sha256:...
```

alias 从配置解析，剩余路径会传递给对应上游 registry。

## Go Module Proxy 请求模型

Go upstream 使用 `type = "go"`：

```hcl
upstreams {
  golang {
    type = "go"
    registry = "https://proxy.golang.org"
  }
}
```

客户端使用：

```bash
GOPROXY=http://localhost:5000,direct
```

Go proxy API 示例：

```text
GET /github.com/pkg/errors/@v/list
GET /github.com/pkg/errors/@v/v0.9.1.info
GET /github.com/pkg/errors/@v/v0.9.1.mod
GET /github.com/pkg/errors/@v/v0.9.1.zip
GET /github.com/pkg/errors/@latest
```

Root Go proxy 请求会按稳定 alias 顺序尝试所有已配置的 Go upstream；存在 `golang` alias 时优先使用它，模块不存在时降级到后续 Go upstream。

`@latest` 和 `@v/list` 使用短 TTL。版本化 `.info`、`.mod` 和 `.zip` 响应按内容 sha256 写入对象存储，并用元数据记录 module/reference 到 digest 的映射。当前不代理 `sum.golang.org`，也不做 VCS direct 拉取。

## 主要组件

```text
Client
  |
  v
Fiber HTTP server
  |
  +-- Registry API handlers
  +-- Go proxy API handlers
  +-- Auth middleware
  +-- Admin UI
  |
  v
Cache services
  |
  +-- OCI manifest cache
  +-- OCI blob cache
  +-- OCI tags cache
  +-- OCI referrers cache
  +-- Go module proxy cache
  |
  v
Storage
  |
  +-- Metadata store: SQLite / MySQL / PostgreSQL
  +-- Object store: local / memory / S3-compatible / SFTP
```

后台服务通过调度器和 worker 池运行：

- 缓存清理和容量控制
- 上游 mirror 探测
- 预测预拉取
- 配置 Redis 或 Valkey 时使用分布式锁

## 元数据模型

元数据层基于 SQL，并使用 `dbx` repository 实现。支持驱动：

- SQLite
- MySQL
- PostgreSQL

元数据围绕 repository 风格接口组织：

- 上游和仓库 catalog 元数据
- manifest 和 tag
- blob 和 repository-to-blob 关系
- pull 记录
- endpoint 健康状态
- 预拉取运行、结果和控制
- Admin UI 和统计使用的聚合读模型

SQL 实现命名为 `SQLStore`。SQLite 特有的路径、DSN 和 pragma 逻辑被隔离在 SQLite driver helper 中。

## 对象模型

blob 对象和元数据分开保存。对象存储可以是：

- 本地文件系统
- memory
- S3 兼容存储
- SFTP

对象 key 尽量按内容寻址。某个对象是否可被某个仓库引用，仍以元数据为准。

## 缓存行为

manifest 缓存 key 会包含 `Accept` 信息，因为同一个 tag 可能根据客户端请求返回不同 manifest media type。

blob 缓存按 digest 内容寻址。返回缓存 blob 前，RegiMux 仍会检查目标仓库是否允许引用该 digest。

tag 和 referrers 使用 TTL，并会通过上游重新校验。

## Mirror 调度

一个上游 alias 可以配置多个 mirror。blob 拉取可以使用基于延迟的选择策略：

- probe 更新 endpoint 延迟和健康状态
- 优先选择成功且健康的 endpoint
- 失败 endpoint 进入冷却窗口
- 内容不一致会临时降低 mirror 优先级

Docker/containerd 客户端本身已经会并发拉取 layer，所以 RegiMux 重点放在选择更好的 mirror、避开慢或不健康的 endpoint。

## 预拉取

预拉取会基于拉取历史预测可能的后续 tag，然后通过正常缓存路径预热 manifest 和 blob。运行记录和结果会存入元数据，并展示在 Admin UI。

预拉取支持：

- 字节预算
- 任务预算
- 仓库数量限制
- 失败退避
- 重试窗口
- Admin UI 取消和重试控制

## 认证

启用后，RegiMux 支持 Docker Registry 认证流程和 `docker login`。用户由本地配置提供。每个用户可以限制可访问的仓库 pattern，例如：

```text
hub/*
ghcr/my-org/*
```

Admin UI 复用同一套配置用户；启用认证后通过 HTTP Basic 保护。

## 依赖注入

应用使用 `dix` 装配。

关键 lifecycle 决策：

- logger、config、auth、cache、upstream、scheduler、worker、admin 和 store 是独立 module
- metadata mapper 是 DI 单例
- `*dbx.DB` 由 DI lifecycle 管理，并在停止时关闭
- SQL repositories 会组合成 `meta.Store` facade，同时保留更窄的 repository 接口给后续消费者使用

## 非目标

- 不支持 push/write Registry
- 不支持 blob upload API
- 不支持 manifest PUT API
- 不支持 delete API
