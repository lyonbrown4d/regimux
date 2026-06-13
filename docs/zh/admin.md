# Admin UI

Admin UI 已嵌入 RegiMux 二进制中。它使用 Fiber template 渲染、内嵌模板、内嵌 i18n 资源，以及 CDN 版本的 Tailwind CSS 和 htmx。

访问：

```text
http://localhost:8080/admin
```

## 安全

当 `auth.enabled = true` 时，Admin UI 会使用和 Registry 认证相同的配置用户做 HTTP Basic 认证。

## 语言和主题

Admin UI 支持中文和英文。语言资源会嵌入到二进制中。

UI 会自动跟随浏览器或操作系统的 light/dark 偏好。

## 页面

当前页面包括：

- 仪表盘
- 上游健康
- 拉取和活动历史
- 缓存状态
- 存储和大 blob
- 调度任务、预拉取运行和预拉取结果
- 手动刷新
- 认证审计
- 有效配置

## 存储统计口径

Admin 的存储和缓存数字来自 metadata 记账，不是实时扫描对象存储目录或 bucket。

- `已落盘 Blob 字节（metadata）` 是已提交 `meta_blobs.size` 记录的汇总。只有 blob 已写入对象存储并提交 metadata 后，才会进入这个数字。
- 存储总量是当前已统计口径：已提交 Blob metadata size 汇总，加 manifest 对象字节（记录为 manifest metadata size）。
- `对象存储字节（list）` 来自 `store.object` 的 CAS 对象实时枚举，仅在当前驱动暴露 object walking 时可用。它适合用于 reconcile 或 dry-run 检查 metadata 记账和对象存储实际内容是否一致；如果驱动不支持枚举或枚举失败，页面会显示不可用。
- `cache.backend` 配置的是 KV 缓存后端（如 Redis 或 Valkey），它和存放已提交 blob/manifest 对象的 `store.object` 不是同一层存储。

## 手动刷新

手动刷新支持生态隔离。它会绕过普通请求的 cache-first 读取路径，主动检查所选上游，并在上游内容发生变化时更新本地缓存：

- `container:<别名>`：OCI 镜像
- `go:<别名>`：Go module proxy
- `npm:<别名>`：npm
- `pypi:<别名>`：PyPI
- `maven:<别名>`：Maven
- `dist:<别名>`：二进制分发物

不同生态的字段语义不同，但均使用统一的 `仓库(repository)` 和 `引用(reference)`：

- `container`：仓库为镜像名路径，如 `library/node`，引用为版本如 `20`。
- `go`：仓库为模块路径，如 `github.com/pkg/errors`，引用为版本或标签如 `v0.9.1`。
- `npm`：仓库为包名（示例 `react`），引用为版本或标签如 `18.2.0`。
- `pypi`：仓库为包名，引用为版本或标签。
- `maven`：仓库为 `group/artifact` 路径，如 `com/fasterxml/jackson/core/jackson-databind`，引用为版本号。
- `dist`：仓库为目录路径或 `dist`，引用为文件名，如 `gradle-8.7-bin.zip`。

示例：

```text
container:hub / repository=library/node / reference=20
go:default / repository=github.com/pkg/errors / reference=v0.9.1
npm:default / repository=react / reference=18.2.0
pypi:default / repository=urllib3 / reference=2.2.0
maven:central / repository=com/fasterxml/jackson/core/jackson-databind / reference=2.16.1
dist:gradle / repository=dist / reference=gradle-8.7-bin.zip
```

请求为异步任务，在结果面板可见运行状态。它适合在后台刷新还没追上时，由管理员主动触发一次上游刷新。

## 任务流程

- 提交：`POST /admin/sync` 创建一个刷新任务，初始状态为 `queued`，并立即调度一个后台一次性任务。
- 轮询：结果面板会在任务状态为 `queued` 或 `running` 时，使用 htmx 自动轮询 `GET /admin/sync/jobs/{id}`（约每 2 秒一次）。
- 终态：
  - `queued`
  - `running`
  - `succeeded`
  - `failed`

完成结果包括：

- `alias` / `repository` / `reference`
- manifest digest 与 media type
- 预热产物计数：
  - 层数
  - blob 数
  - 子 manifest 数
- 耗时

## 入参规则

- `repository` 可用 `repo:tag` 或 `repo@digest` 形式携带引用，系统会自动拆分到 `Reference`。
- `reference` 为空时默认使用 `latest`。
- container 生态会按 manifest 路径解析仓库并应用该 alias 的默认命名空间（如 `library/*`）。
- 非 container 生态会在别名校验通过后直接透传 `repository`/`reference`。

## 错误返回

刷新入口会返回对应状态码：

- `400`：参数校验失败（例如 repository 为空）
- `503`：未配置手动刷新能力
- `502`：调度或提交失败
- `404`：查询不存在的 job id

当前手动刷新任务仅保存在内存中并通过轮询接口暴露，不会单独持久化为历史任务表。
