# 容器真实验收记录（2026-10-15）

本记录汇总 `tmp/container-acceptance/reports/` 中的真实容器证据。以下客户端、全量 Go 测试和手动清理结果对应 `2fbcad8` 验收镜像；最终代码提交与清理复验结果待补。

## 已通过

| 场景 | 结果 | 日志证据 |
|---|---|---|
| 六生态真实客户端 | **通过**。Maven 两个独立本地仓库编译并验证 POM/JAR/SHA-1；OCI Docker 两次拉取 busybox，第二次命中清单缓存；npm 两个客户端并发安装 lodash；Go 两个模块缓存并发下载 rsc.io/quote 的 info/mod/zip；PyPI pip/uv 下载与锁定、HEAD/Range/If-Range/416 校验通过；dist 下载 Node.js 发行包并核对 SHA-256。 | `maven-2fbcad8-live.log`、`oci-2fbcad8-live.log`、`native-dependencies-2fbcad8-live.log`、`pypi-dist-replica-2fbcad8-live.log` |
| 双实例共享缓存 | **通过**。两个 RegiMux 副本返回相同内容，随后命中共享 S3 缓存。 | `pypi-dist-replica-2fbcad8-live.log` |
| Go 全量测试 | **通过**。39 个包测试通过，另有 9 个包无测试文件；退出码为 0。 | `go-full-2fbcad8.log` |
| 原始 75.5 MB wheel | **通过**。冷 Range 返回 206 且未发布部分缓存；完整响应头 0.721 秒到达，14.963 秒接收完 75,545,685 字节，并校验热缓存及 S3。 | `pypi-large-wheel-live.log` |
| 默认手动共享缓存清理 | **通过**。跨生态清理淘汰 8 个逻辑组、退休 42 条引用，删除 15 个对象、共 28,811,834 字节；重复提交幂等，跨生态共享 CAS 对象保留。 | `cleanup-manual-live.log`、`cleanup-manual-service-final.log` |
| S3 不可用降级 | **通过**。停止并确认 S3 服务退出后，热 Maven/PyPI 内容与快照一致，SQL 断言未缓存的冷 Maven POM 请求成功；耗时分别为 4.468、2.852、1.311 秒。finally 恢复 S3 后健康检查通过。 | `cache-outage-stop.log`、`cache-outage-verify.log`、`cache-outage-restore.log` |
| 镜像回退边界 | **通过本次受控配置场景**。受限 Maven 镜像对 POM/SHA-1 返回 503、Docker Hub 镜像对 busybox 清单返回 403 后，公开备用源回退成功。OCI 公开 403 回退仅适用于显式配置 `mirror_forbidden_policy = "public"` 的 Docker Hub 别名；这不证明私有凭据拒绝或所有主源 403 路径。 | `restricted-mirror-live.log`、`maven-2fbcad8-live.log`、`oci-2fbcad8-live.log` |

## 尚待完成

- OCI transient cache 失效修复后的**手动清理复验**尚未记录；此前默认手动清理虽完成 SQL/S3 断言，但新加入的 OCI Redis envelope 失效修复需要重新验收。
- scheduler disabled 与定时 cron 清理场景仍待复验，不能据此声称所有清理模式通过。
- S3 不可用降级需在最终镜像上重跑。
- 最终实现提交、镜像 ID 和上述场景结果将在复验后补录。

此前 pip 26 的 PEP 658 `.metadata` URL 处理问题由 `bfbce1f` 修复：pip 会把 `.metadata` 追加到 wheel URL 末尾，旧代理将其误判为内部 SHA 查询参数。URL 规范化的单测与 pip/uv 容器复验通过，证据见 `pypi-pep658-red.log`、`pypi-pep658-green.log` 和 `pypi-live-bfbce1f.log`。

此前全局 ETag 中间件读取完整流式 200 正文，导致大 wheel 响应头超时；修复后六个代理命名空间绕过通用 ETag/压缩，保留制品验证器和 Range 行为，管理页面仍使用通用 ETag。2f 镜像真实大 wheel 验收见上表；回归证据见 `artifact-middleware-red.log`、`artifact-middleware-green.log`。
