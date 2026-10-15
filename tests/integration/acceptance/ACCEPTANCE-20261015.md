# 容器真实验收记录（2026-10-15）

本记录仅汇总 `tmp/container-acceptance/reports/` 中已有的容器日志。实现提交为 `22ee8d2`（依赖代理与共享缓存清理）和 `bfbce1f`（pip 元数据 URL 规范化及容器验收）。

| 场景 | 结果 | 日志证据 |
|---|---|---|
| Maven | **通过**。两个独立本地仓库分别编译引用 `commons-io:2.16.1` 的 Java 17 源文件；均验证 POM、JAR、SHA-1，第二轮从代理缓存命中。 | `maven-live.log` |
| OCI / Docker | **通过**。真实 Docker 客户端两次拉取 `busybox:1.36.1`，摘要相同；第二次清除本地标签后重新拉取，RegiMux 日志记录清单缓存命中。 | `oci-live.log` |
| npm | **通过**。两个独立客户端缓存并发下载 lodash 4.17.21；容器退出码为 0。 | `npm-live-bfbce1f.log` |
| Go 客户端代理 | **通过**。两个独立模块缓存并发下载 rsc.io/quote v1.5.2，分别检查 info、mod、zip；容器退出码为 0。 | `go-live-bfbce1f.log` |
| Go 全量容器测试 | **待复验**。现有冷编译日志中 API 路由测试和上游探测测试因超时失败，不能作为最终通过结果。 | `go-container.log` |
| dist | **通过**。下载 Node.js 发行包、校验 SHA-256，并复用 S3 缓存。 | `dist-live.log` |
| 双实例共享缓存 | **通过**。两个实例返回相同内容，随后均命中共享 S3 缓存。 | `replica-live.log` |
| PyPI / pip / uv | **通过**。两个独立 pip 缓存下载 six 1.17.0、requests 2.32.3 及其依赖；uv lock、lock --check、sync --frozen、导入版本检查通过；SHA-256、HEAD、Range、If-Range、416 与完整缓存均通过。 | `pypi-live-bfbce1f.log` |
| PyPI 冷 Range | **通过**。未缓存的 six 1.16.0 返回 206，SQL/S3 未发布片段；完整下载后哈希和 S3 缓存通过。 | `pypi-cold-range-live.log` |
| S3 不可用降级 | **通过**。停止且确认 S3 容器为 exited 后，热 Maven、热 PyPI 内容哈希与快照一致，冷 Maven POM 成功；耗时分别 5.073、4.185、4.960 秒。finally 恢复 S3，bucket 与 PostgreSQL 就绪检查通过。 | `cache-outage-stop.log`、`cache-outage-verify.log`、`cache-outage-restore.log` |

## 受控故障与回退边界

受控上游日志确认 Maven 镜像对 POM/SHA-1 请求返回 503，Docker Hub 镜像对 busybox 清单返回 403；Maven 编译和 Docker 拉取随后均成功。该验收配置用受信任的公开备用源验证故障回退。OCI 的公开 403 回退只针对显式启用 `mirror_forbidden_policy = "public"` 的 Docker Hub 别名；这不构成对私有凭据拒绝或主源 403 的通用放行。现有日志证明本次配置场景，不单独证明私有认证拒绝的所有路径。证据见 `restricted-mirror-live.log`、`maven-live.log` 和 `oci-live.log`。

首次真实 pip 暴露了额外的 PEP 658 问题：pip 26 将 `.metadata` 追加在整个 wheel URL 末尾，落入内部 SHA 查询参数，代理因而返回 wheel 内容并触发元数据哈希不匹配。`bfbce1f` 增加窄范围 URL 规范化；对应单测 red/green 和新镜像真实 pip/uv 复验均已通过。原失败证据保留在 `pypi-live.log`、`pypi-pep658-red.log`，单测通过证据为 `pypi-pep658-green.log`。

新镜像 `regimux-acceptance:bfbce1f` 的 ID 为 `sha256:76013d08ea9aaf86d2212af9a0ec851ea2d9fb76f6959c3ad776da36aad9e737`，revision 标签为完整提交 `bfbce1f293932a99faa956fba5ab9f2797be20e9`。Maven、OCI、dist、双实例验收先在 `22ee8d2` 完成；随后唯一生产代码变更为 PyPI URL 规范化，其余客户端复验及后续清理场景使用新镜像。

原始 75.5 MB wheel 的补充验收又发现全局 ETag 中间件读取完整流式 200 正文，造成代理等待响应头超时；同一容器直连上游约 0.570 秒收到响应头。默认中间件回归测试在五个依赖生态重现超时，原先已豁免的 OCI 路径通过。修复使用现有 Fiber 中间件的 Next 接口，使六个代理命名空间跳过通用 ETag/压缩，保留制品自身的验证器和字节范围语义；管理页面仍保留通用 ETag。容器内完整 API 包回归通过（4.011 秒），新镜像真实复验待执行。证据：`pypi-large-wheel-live.log`、`pypi-large-direct-control.log`、`artifact-middleware-red.log`、`artifact-middleware-green.log`。

## 待执行

- 完成 Go 全量容器测试复验。
- 完成 #4 原始 75.5 MB wheel 的冷/热 Range 和完整哈希验收。
- 执行三种共享缓存清理场景及 S3 不可用时的下载降级场景。

首次 Maven `dependency:resolve` 验收因 Maven 插件引导耗时而中断；后续已改为直接执行 `maven-compiler-plugin:3.13.0:compile`，以上 Maven 通过结果来自后续两次真实编译，而非中断日志。
