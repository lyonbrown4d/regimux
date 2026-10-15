# 容器真实验收记录（2026-10-15）

最终代码：`ffdfb201005ae9844467c592674533411f74803d`。验收镜像：`regimux-acceptance:ffdfb20`，ID `sha256:88ad1799dbbac81e2118171e5b596482df8fbb3f3b896edc4fa2e38f60bb5ab1`。镜像与提交的 675 个源文件核对无差异。证据位于忽略目录 `tmp/container-acceptance/reports/`。

| 类别 | 结果 | 证据 |
|---|---|---|
| Maven 客户端 | **通过**。两个独立本地仓库编译并校验 POM、JAR、SHA-1；耗时 29.342 秒和 12.584 秒。 | `maven-ffdfb20-live.log` |
| OCI / Docker | **通过**。两次拉取 busybox；第二次命中清单缓存。 | `oci-ffdfb20-live.log` |
| npm、Go 客户端 | **通过**。真实 npm 与 Go 客户端均退出码 0。 | `native-dependencies-ffdfb20-live.log` |
| PyPI、dist、双实例 | **通过**。pip/uv 下载和锁定、哈希及 Range 检查通过；Node.js 发行包 SHA-256 校验通过；两个副本返回相同内容并复用共享 S3 缓存。 | `pypi-dist-replica-ffdfb20-live.log` |
| #4 原始大 wheel | **通过**。统一清理后重新验证冷 Range：206、64 KiB，未发布局部 SQL/S3 缓存；完整流式下载 75,545,685 字节，SHA-256、热 Range、HEAD 和完整 S3 对象均通过。 | 响应头 0.715 秒到达，正文 302.256 秒收完；`pypi-large-wheel-ffdfb20-reset.log`、`pypi-large-wheel-ffdfb20-live.log` |
| 默认手动清理 | **通过**。使用真实 catalog 的 20 个预热 case；两个请求均返回 202，重复触发无额外删除。 | 8 组、39 条引用、14 个 S3 对象、28,811,794 字节；`cleanup-manual-ffdfb20-live.log`、`cleanup-manual-ffdfb20-service.log`、`oci-cache-after-cleanup-ffdfb20.log` |
| Scheduler disabled 双实例清理 | **通过**。两请求均返回 202，由分布式锁串行执行；一轮完成删除，另一轮删除 0；重复触发幂等。 | 8 组、39 条引用、14 个 S3 对象、28,811,794 字节；`cleanup-manual-disabled-ffdfb20-live.log`、`cleanup-manual-disabled-ffdfb20-service.log`、`oci-cache-after-disabled-cleanup-ffdfb20.log` |
| 定时 cron 清理 | **通过**。未调用手动清理 API，等待 scheduler 实际触发；完成后恢复默认每日 03:00（Asia/Shanghai），双实例健康。 | 8 组、39 条引用、14 个 S3 对象、28,811,794 字节；`cleanup-cron-ffdfb20-live.log`、`cleanup-cron-ffdfb20-service.log`、`oci-cache-after-cron-ffdfb20.log`、`cleanup-default-restored-ffdfb20-registration.log` |
| S3 不可用降级 | **通过**。确认 S3 处于 exited 后，热 Maven、热 PyPI 内容与快照一致，冷 Maven 下载成功；finally 恢复 S3 和 PostgreSQL 并通过就绪检查。 | 热 Maven 14.543 秒、热 PyPI 2.646 秒、冷 Maven 1.351 秒；`cache-outage-ffdfb20-prepare.log`、`cache-outage-ffdfb20-warm-hits.log`、`cache-outage-ffdfb20-stop.log`、`cache-outage-ffdfb20-verify.log`、`cache-outage-ffdfb20-restore.log` |
| Go 全量测试 | **通过**。39 个包测试通过，9 个包无测试文件，退出码 0；受影响的 cache backend/cachegc/container 包复验通过。 | `go-full-lint-cleanup.log`、`go-final-context-helpers.log`、`go-final-lint-two-findings.log` |
| Go lint | **通过**。golangci-lint v2.14.0，0 issues。 | `golangci-lint-final.log` |
| GoReleaser / 静态工具 | **通过**。官方 GoReleaser v2.18.2 在 Linux 构建容器中执行 CI 配置检查；Pyright 0 错误，shell、uv lock 和 Compose 工具环境验证通过。 | `goreleaser-check-final.log`、`pyright-acceptance-final.log`、`tools-validation-final.log` |

三种清理后 OCI Redis transient keys 均为 0。cron 夹具首次预热 dist archive 遇到 180 秒上游超时，未启动 cron；peer 冷下载 45 秒控制也超时。随后重跑完成预热并实际触发 cron，通过清理断言。最终原生客户端组合及清理均通过，但仍观察到 dist 冷下载偶发超时，本次不将其声称为已解决。证据见 `cleanup-cron-ffdfb20-upstream-timeout.log`、`cleanup-cron-ffdfb20-live.log`、`dist-ffdfb20-peer-cold-control.log`。

同一 75.5 MB wheel 在 2f 阶段正文传输为 14.963 秒，最终镜像复验为 302.256 秒；两次完整 SHA-256 均正确且响应头均在一秒内到达。此次验证流式响应和缓存正确性，不对公网传输吞吐作保证。早期对照见 `pypi-large-wheel-live.log`。

清理在跨请求 CAS 更新与删除之间没有全局线性化屏障；conditional revision checks 可发现并发变更并延后对象回收。Pull 记录作为审计历史保留，不随包引用清理删除。
