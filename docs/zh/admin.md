# Admin UI

Admin UI 已嵌入 RegiMux 二进制中。它使用 Fiber template 渲染、内嵌模板、内嵌 i18n 资源，以及 CDN 版本的 Tailwind CSS 和 htmx。

访问：

```text
http://localhost:5000/admin
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
- 手动同步
- 认证审计
- 有效配置

## 手动同步

手动同步现已支持生态隔离（Manual Sync 是生态感知）：

- `container:<别名>`：OCI 镜像
- `go:<别名>`：Go module proxy
- `npm:<别名>`：npm
- `pypi:<别名>`：PyPI
- `maven:<别名>`：Maven

不同生态的字段语义不同，但均使用统一的 `仓库(repository)` 和 `引用(reference)`：

- `container`：仓库为镜像名路径，如 `library/node`，引用为版本如 `20`。
- `go`：仓库为模块路径，如 `github.com/pkg/errors`，引用为版本或标签如 `v0.9.1`。
- `npm`：仓库为包名（示例 `react`），引用为版本或标签如 `18.2.0`。
- `pypi`：仓库为包名，引用为版本或标签。
- `maven`：仓库为 `group/artifact` 路径，如 `com/fasterxml/jackson/core/jackson-databind`，引用为版本号。

示例：

```text
container:hub / repository=library/node / reference=20
go:default / repository=github.com/pkg/errors / reference=v0.9.1
npm:default / repository=react / reference=18.2.0
pypi:default / repository=urllib3 / reference=2.2.0
maven:central / repository=com/fasterxml/jackson/core/jackson-databind / reference=2.16.1
```

请求为异步任务，在结果面板可见运行状态。手动同步会按生态协议预热对应缓存路径，并将结果记录到元数据中。
