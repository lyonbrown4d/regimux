# RegiMux Roadmap

RegiMux 定位为面向研发和 CI 环境的只读 Dependency Proxy（依赖代理）。产品继续保持缓存导向，container registry、Go modules、npm、PyPI 和 Maven 都作为一等依赖生态。

## 近期

### 研发依赖代理

- 保持 OCI / Docker Registry V2 API `/v2/{containerAlias}/...`，作为稳定 container 依赖代理路径。
- 使用独立顶层生态配置块：`container`、`go`、`npm`、`pypi` 和 `maven`，每个块定义一个或多个依赖代理 alias。
- 将 proxy、mirror、probe 和 prefetch 行为通过 `dix` 注册的生态 runtime 承载；调度器按 runtime capability 分发，而不是直接导入具体生态实现。
- endpoint service 和 runtime/capability 实现都保留在各自生态子包内。
- container、Go、npm、PyPI 和 Maven 都注册 runtime job/capability；container 具备预测性定时 `prefetch`，Go、npm、PyPI 和 Maven 通过同一 runtime 抽象共享定时 endpoint `probe` 和 recent-pull prefetch rewarm。
- 增加 Go module proxy read-through cache，路径为 `/go/{goAlias}/{module}/@v/...`。已完成。
- 默认示例 Go alias 指向 `https://proxy.golang.org`。客户端可通过 `GOPROXY=http://localhost:8080/go/{goAlias}` 使用。
- 将 Go proxy 响应按内容 sha256 写入对象存储，并通过元数据记录请求路径到对象 digest 的映射。已完成。
- npm 已在 `/npm/{npmAlias}/...` 可用，覆盖 packument、dist-tags、scoped package、tarball URL rewrite 和 integrity。
- PyPI 已在 `/pypi/{pypiAlias}/...` 可用，实现 PEP 503 simple index 缓存、包名 normalize 和文件链接重写。
- Maven 已在 `/maven/{mavenAlias}/...` 可用，实现 read-through 仓库布局缓存，支持 release 制品、`maven-metadata.xml` 和 checksum 文件。
- 继续用真实包管理器客户端和各生态依赖解析边界场景强化 npm、PyPI 和 Maven 兼容性。

### S3 兼容对象存储

- 增加 S3 兼容对象存储驱动，覆盖 AWS S3、MinIO、R2 和 OSS 兼容部署。已完成。
- 保留本地文件系统对象存储，作为单节点默认方案。已完成。
- 支持 bucket、endpoint、region、凭证、path style 和对象 key prefix。已完成。
- 保留读写时的 digest 校验。已完成。
- 增加 MinIO Docker Compose 集成示例。

### SFTP 对象存储

- 增加基于 `github.com/spf13/afero/sftpfs` 的 SFTP 对象存储驱动。已完成。
- 复用 `afero.NewBasePathFs`，让 `store.object.path` 表示远端对象根目录。已完成。
- 要求通过 `known_hosts_path` 或固定 `host_key` 做主机密钥校验。已完成。
- 增加带 SFTP server 容器的集成示例。

### 缓存清理和容量控制

- 基于元数据扩展对象缓存容量水位。已完成。
- 当对象已经丢失时，删除孤立 blob 元数据。已完成。
- 在日志和 Admin UI 中支持 dry-run 清理报告。
- 基于最近访问时间淘汰 blob 和 repo-to-blob 关系。blob 已完成。

### Registry 客户端兼容性测试

- 增加 Docker CLI、nerdctl/containerd 和 ORAS 端到端测试。
- 覆盖 Docker login、manifest HEAD/GET、blob range reads、多架构镜像、tags 分页和 referrers。
- 保留协议级单元测试，但发布前用真实客户端验证行为。

### Admin 运维操作

- 增加针对 repository、tag、digest 和孤立对象的手动清理动作。
- 增加 mirror 重新探测控制。
- 增加上游、缓存、调度器和元数据操作的近期错误视图。
- 增加清理和预拉取后台任务历史。

## 中期

### 预拉取策略控制

- 增加单次运行的字节预算、任务预算和仓库限制。
- 增加预测镜像失败退避和重试窗口。
- 持久化预拉取运行历史和候选结果。
- 在 Admin UI 暴露取消和重试控制。

### Mirror 调度优化

- 持久化 endpoint 健康快照，避免重启后所有 mirror score 冷启动。
- 按 endpoint 和 repository 统计成功率。
- 为重复失败 mirror 增加 circuit breaker 窗口。
- 为后台探测增加 jitter，避免同步突发。
- 检测 mirror 内容不一致并临时降低受影响 endpoint 的优先级。

### 元数据模型扩展

- 当 Admin/query 功能需要时，将 upstream 和 repository 元数据提升为一等表。
- 除非确实需要运行时元数据，否则继续以配置作为 upstream 定义的事实来源。
- 增加 repository 级聚合统计，包括 pull、bytes、blob links 和 last activity。

## 后续

### 认证和策略

- 将 Registry pull 权限和 Admin 权限拆分。
- 增加 password hash 生成工具。
- 增加 token revoke 或短生命周期 token 轮换能力。
- 本地配置模型稳定后，再考虑 OIDC 或 LDAP 等外部身份源。

### 可观测性

- 提供 Prometheus metrics 对应的 Grafana dashboard。
- 增加慢上游请求和慢 DB 操作摘要。
- 如果 observabilityx tracing 在 HTTP、cache、upstream 和 DB 层统一落地，则增加 trace 支持。
- 增加 diagnostics export endpoint 或 Admin 操作。

### 部署

- 增加 Helm chart 示例。
- 增加 Redis/Valkey、PostgreSQL/MySQL 和 S3/MinIO 生产部署说明。
- 在 CI 中覆盖支持的元数据驱动和对象存储驱动。
- 保持 archive、deb、rpm、Windows exe 和 Docker 镜像发布产物一致。

## 当前非目标

- 暂不支持 push/write registry；RegiMux 继续保持只读。
- 暂不支持 npm、PyPI、Maven 或 Go modules 的包发布流程；RegiMux 是依赖读取代理，不是上游包创作/发布服务。
