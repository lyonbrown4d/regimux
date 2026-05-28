# RegiMux Roadmap

RegiMux 会继续聚焦只读 OCI / Docker Registry V2 代理镜像。下面记录的是让它更适合作为长期运行生产服务的后续工作。

## 近期

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

