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

手动同步会通过配置的缓存路径预热镜像：

```text
{containerAlias}/library/node:20
{containerAlias}/gitlab/gitlab-ce:latest
```

它会拉取 manifest 和关联 blob，并将结果记录到元数据中。
