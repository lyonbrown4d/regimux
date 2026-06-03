# 设计

## 定位

RegiMux 是一个只读研发依赖缓存网关。当前稳定 handler 包括 OCI / Docker Registry V2 代理镜像和 Go module proxy read-through cache。配置按生态拆分为 `container`、`go`、`npm`、`pypi` 和 `maven` 块。

OCI 部分对外暴露兼容 Registry 的 pull API，并通过 container alias 将请求路由到不同 container 上游 registry。Go 部分在 `/go/{goAlias}/...` 暴露 Go module proxy 协议，并将请求路由到 `go` 配置块下的上游。

RegiMux 不是 push registry。上传、manifest 写入和删除 API 当前都不在范围内。

## OCI 请求模型

镜像名使用仓库路径的第一段作为 container alias：

```text
localhost:5000/{containerAlias}/library/alpine:latest
localhost:5000/{containerAlias}/org/app:v1.2.3
```

Registry API 示例：

```text
GET /v2/{containerAlias}/library/alpine/manifests/latest
GET /v2/{containerAlias}/library/alpine/blobs/sha256:...
GET /v2/{containerAlias}/library/alpine/tags/list
GET /v2/{containerAlias}/library/alpine/referrers/sha256:...
```

container alias 从 `container` 配置块解析，剩余路径会传递给对应上游 registry。

## Go Module Proxy 请求模型

Go upstream 放在 `go` 生态配置块下：

```hcl
go {
  default {
    registry = "https://proxy.golang.org"
  }
}
```

客户端使用：

```bash
GOPROXY=http://localhost:5000/go/{goAlias},direct
```

Go proxy API 示例：

```text
GET /go/{goAlias}/github.com/pkg/errors/@v/list
GET /go/{goAlias}/github.com/pkg/errors/@v/v0.9.1.info
GET /go/{goAlias}/github.com/pkg/errors/@v/v0.9.1.mod
GET /go/{goAlias}/github.com/pkg/errors/@v/v0.9.1.zip
GET /go/{goAlias}/github.com/pkg/errors/@latest
```

Go alias 只在 `go` 配置块内解析，不与 container、npm、PyPI 或 Maven alias 共享命名空间。

`@latest` 和 `@v/list` 使用短 TTL。版本化 `.info`、`.mod` 和 `.zip` 响应按内容 sha256 写入对象存储，并用元数据记录 module/reference 到 digest 的映射。当前不代理 `sum.golang.org`，也不做 VCS direct 拉取。

## 其他生态路径前缀

npm、PyPI 和 Maven 都使用各自独立的 alias 命名空间：

```text
GET /npm/{npmAlias}/...
GET /pypi/{pypiAlias}/...
GET /maven/{mavenAlias}/...
```

## 生态 Runtime 抽象

registry、mirror、probe 和 prefetch 等跨生态能力通过生态 runtime 暴露，而不是写死在调度器里。每个 runtime 负责一个生态的协议细节，并声明自己支持的 capability。

调度器通过 `dix` 获取 runtime 集合，并按 capability 注册后台任务：

- `probe`：对配置了 mirror 探测的 alias 采集 endpoint 健康状态和延迟。
- `prefetch`：沿用客户端请求的缓存路径，预热可能即将访问的制品。

当前 capability 覆盖有意保持不对称。container runtime 支持预测性 `prefetch`，因为 OCI 拉取已经依赖 mirror 打分以及 manifest/blob 预热。Go、npm、PyPI 和 Maven 支持通用 endpoint `probe` capability，也通过同一 runtime 注册边界支持 recent-pull `prefetch` rewarm；后续增加各生态自己的版本预测时不需要改调度器装配。

## 主要组件

```text
Client
  |
  v
Fiber HTTP server
  |
  +-- Registry API handlers
  +-- Go proxy API handlers
  +-- npm / PyPI / Maven proxy handlers
  +-- Auth middleware
  +-- Admin UI
  |
  v
Ecosystem runtimes
  |
  +-- container runtime: Registry V2, mirrors, probe, prefetch
  +-- Go runtime: module proxy cache, endpoint probe
  +-- npm runtime: registry cache, endpoint probe
  +-- PyPI runtime: simple index and file cache, endpoint probe
  +-- Maven runtime: repository layout cache, endpoint probe
  |
  v
Storage
  |
  +-- Metadata store: SQLite / MySQL / PostgreSQL
  +-- Object store: local / memory / S3-compatible / SFTP
```

后台服务通过调度器和 worker 池运行：

- 缓存清理和容量控制
- 基于 capability 的 mirror 探测
- 基于 capability 的预测预拉取
- 配置 Redis 或 Valkey 时使用分布式锁
- 配置远程 cache backend 时使用 Redis/Valkey endpoint 健康热状态

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

endpoint 健康状态以 SQL 为持久化事实来源。cache backend 配置为 Redis 或 Valkey 时，probe 更新也会写入共享热状态层，让多个副本避免 endpoint score 冷启动，并更快共享低延迟 mirror 排序。

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

一个 container alias 可以配置多个 mirror。container runtime 会在 alias 启用探测时声明 `probe` capability。blob 拉取可以使用基于延迟的选择策略：

- probe 更新 endpoint 延迟和健康状态
- 优先选择成功且健康的 endpoint
- 失败 endpoint 进入冷却窗口
- 内容不一致会临时降低 mirror 优先级

Docker/containerd 客户端本身已经会并发拉取 layer，所以 RegiMux 重点放在选择更好的 mirror、避开慢或不健康的 endpoint。

## 预拉取

container 预拉取会基于拉取历史预测可能的后续 tag，然后通过正常缓存路径预热 manifest 和 blob。依赖生态 prefetch 当前会回放近期 pull history，并通过对应生态 proxy 缓存路径刷新完全相同的 Go/npm/PyPI/Maven 制品。调度器通过 runtime 的 `prefetch` capability 调用这两类任务，因此后续可以在同一种任务形态后面补各生态自己的版本预测逻辑。运行记录和结果会存入元数据，并展示在 Admin UI。

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
{containerAlias}/*
{containerAlias}/my-org/*
```

Admin UI 复用同一套配置用户；启用认证后通过 HTTP Basic 保护。

## 依赖注入

应用使用 `dix` 装配。

关键 lifecycle 决策：

- logger、config、auth、ecosystem runtime、cache、upstream、scheduler、worker、admin 和 store 是独立 module
- 各生态 runtime 实现通过 `dix` 注册；调度器消费注册后的 runtime 集合，而不是导入具体生态 handler
- metadata mapper 是 DI 单例
- `*dbx.DB` 由 DI lifecycle 管理，并在停止时关闭
- SQL repositories 会组合成 `meta.Store` facade，同时保留更窄的 repository 接口给后续消费者使用

## 非目标

- 不支持 push/write Registry
- 不支持 blob upload API
- 不支持 manifest PUT API
- 不支持 delete API
