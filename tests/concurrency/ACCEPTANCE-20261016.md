# 高并发验收报告（草稿）

状态：**验收进行中**。本报告记录修复证据及当前压力测试结果；不代表最终通过。最终 48 项矩阵、完整检查与结论待补。

## 范围与复现方法

测试夹具用隔离的内部 Compose 网络连接六种上游别名：OCI、Go、npm、PyPI、Maven、Node.js dist。合成归档默认包含 131,072 字节确定性不可压缩负载。客户端核验 HTTP 状态、完整正文 SHA-256，或被代理改写后的 metadata markers；混合方法用例另核对 HEAD 空正文。fixture 统计按真实上游路径和 GET/HEAD 分开计数。

- SQLite：单个 RegiMux 实例、本地元数据和对象存储、Redis lease。
- PostgreSQL、MySQL：各两个 RegiMux 实例，共享对应数据库、SeaweedFS S3 和 Redis。
- 每个 RegiMux 限额 2 CPU / 1 GiB；k6 客户端 4 CPU / 3 GiB。README 说明 Docker 资源样本是观测最大值，并非连续峰值。
- 预定义通过条件：单对象冷请求每个 artifact 恰好一次上游 GET；OCI manifest 和流式 PyPI wheel 因按进程合并，允许每副本一次。热流量预热后要求零上游 GET。双副本例外上限为 2。未在测试后放宽阈值。
- 预期矩阵：每 profile 13 个单对象冷 512 VU、512/1024 VU 各 30 秒热流量，以及 512 VU OCI GET/HEAD 混合；3 profiles 共 48 项。当前未完整执行。

建议按 [README](README.md) 中的顺序启动一个 profile，并通过 [run-matrix.ps1](run-matrix.ps1) 执行：`./tests/concurrency/run-matrix.ps1 -Backend sqlite -RunId <unique-id>`。切换 PostgreSQL/MySQL 前停止上一个 profile。脚本遇首个失败即停，报告目录拒绝覆盖；k6 行为和校验规则见 [load.ts](load.ts)。日志与报告位于本地 gitignored `tmp/concurrency-20261016/reports`，不随仓库远程链接分发。

## 根因修复与红绿证据

| 问题 | 修复概要 | 证据 |
|---|---|---|
| FillTracker owner 被取消会毒化仍在等待的健康 follower；错误接管判断过宽 | owner 请求取消才允许接管；backend timeout 不触发重试波。 | `fill-cancellation-green.log`：512 虚拟时间回归通过；修复前失败见 `fill-cancellation-red.log`。 |
| OCI GET/HEAD 共用 flight 会串用响应；共享请求上下文受单个调用方取消影响 | GET 与 HEAD 分 flight；manifest/blob/token/tag refresh 用 `singleflightx.DoContext`，隔离 caller cancellation。最后 waiter 取消会先移除 flight 再取消共享上下文。 | `oci-cancel-red.log`、`oci-manifest-cancel-green.log`；独立 review 核对 waiter 生命周期。 |
| npm tarball base path 重复，query/fragment 丢失；PyPI 根相对 wheel 地址被当作本地代理路径 | 用 `net/url` 解析并组合 URL。 | `metadata-links-red.log` → `metadata-upgrade-follow-green.log`；升级跟链用例实际跟随 canonical npm 与 PyPI 链接，下载字节完整 SHA-256 匹配。 |
| 私有 metadata header 未持久化或未安全升级 | npm:2、pypi:2 格式写入 SQL 白名单持久化；旧 index 自动重填，package archives 保留。 | `metadata-upgrade-follow-green.log` 实际跟链验包；`new-fixes-targeted-green.log`。 |
| Redis FillLease 返回类型不同，生产 KV 不满足 artifact fill lease 类型断言 | 类型别名指向 canonical `backend.Lease` / `LeaseBackend`。 | `backend-contract-red.log` → `backend-contract-green.log`。 |
| Fiber pooled request 的路径绑定参数在请求间复用而改变；Maven alias 实际变成 `odejs/v` | API 启用 `Immutable: true`，确保异步使用的路由参数拥有独立值。 | `request-lifetime-pooled-red.log` 精确复现；128 个真实 HTTP keep-alive 请求回归已纳入并通过本次全量 Go 检查。 |
| Store.Get 并发读取重复访问 SQL 元数据；共用可变响应会污染各请求 | 按 key/tag/manifest/touch 合并不可变 SQL snapshot；每个 caller 仍独立打开对象 reader，并独立处理 header 与过期判断。Put 提交后 `reads.Forget`，避免新读加入旧快照。 | `read-fanin-red.log` → `read-fanin-green.log`：tag/manifest/touch SQL 读取从各 512 次降至各 1 次；对象 reader 仍由 512 个 caller 各自打开。并发读写见 `read-write-red.log` / `read-write-green.log`。 |
| PyPI wheel 流式填充 owner 中止后，健康 follower 接管造成重复 upstream 拉取；不可缓存路径也可能放大重试 | 将替换限制为一次，重试前复查 owner cache，保留 5 分钟共享等待预算；不可缓存 404 fallback 仍有界。 | `pypi-takeover-and-read-red.log`：upstream 调用 129 次，预期 2；`pypi-takeover-and-read-green.log`：接管及重复 404 回归通过。 |
| PyPI 已缓存 wheel 热路径绕过通用 Store.Get，导致 SQL tag 查询扇出 | wheel 缓存读取改用 `artifactcache.LookupManifest` 复用 metadata flight，并调用 Touch；复制记录 header，不打开对象 reader；Put 后清除共享 flight。 | 同一 red 日志：128 次 tag 查询，预期 1；green 日志验证 tag 计数、元数据 reader 隔离和接管路径。targeted race 连续 3 轮通过；最终源码的全量 Go 和 race 检查也通过，artifactcache race 4.035s、PyPI 36.204s。独立只读审查无 Critical/Important。 |
| PyPI owner 二次查缓存叠加存储故障等待预算 | 区分正常未命中与读取失败；正常 miss 保留竞选后的检查，已失败的缓存读取直接回退上游。 | `storage-budget-regression-red.log`：读取卡住时响应头等待约 4 秒，超过原有 3 秒断言；`storage-budget-regression-green.log`：相关 race 连续 3 轮通过，artifactcache 1.036s、PyPI 29.675s。 |
| 双副本 catalog 聚合后写入存在旧快照覆盖新统计的窗口 | 用 upstream 行事务锁串行同一上游下的刷新；聚合和更新显式绑定同一 `dbx.Tx`。SQLite 事务先取得写锁；MySQL 锁前不建立普通读取快照。 | PostgreSQL/MySQL 的 `catalog-replica-*-red.log` 均测出两次 pull 后上游计数只有 1；对应 green 的 `-race -count=3` 分别 2.311s、3.842s。SQLite 相关 race 回归 8.334s。独立只读审查无 Critical/Important。MySQL 此处先临时显式开启多语句以隔离下面的迁移缺陷，默认 DSN 路径另外验证。 |
| MySQL 默认 DSN 无法执行含多条 ALTER 的已发布迁移 | 复用 `mysql.ParseDSN` / `NewConnector` 与现有 migrate runner，仅为迁移连接开启多语句；保留业务连接原 DSN。迁移 SQL 及 hash 不变。 | `mysql-migration-bootstrap-red.log`：V202606120001 执行报 1064。`mysql-bootstrap-and-catalog-green.log`：默认 DSN 在全新数据库首次迁移、重复打开、业务连接拒绝多语句以及双副本 catalog 的 race 连续 3 轮通过（41.009s）。独立审查无 Critical/Important。 |
| 多副本同时初始化全新数据库会竞争执行 DDL | 整个迁移入口持有数据库 session lock，覆盖 history、hash 校验和 DDL。PostgreSQL 复用已有 Goose；MySQL 用数据库级命名锁。等待可取消；获取或释放锁失败时丢弃专用连接，保留原始错误。 | `startup-postgres-red.log`、`startup-mysql-red.log`：两者均为 8 个同步 Open 中 7 个失败，分别出现类型唯一约束、history 主键和重复列冲突。修复后各用 3 个全新数据库，每轮 8 个同时 Open 全部成功（PG 41.131/41.118/41.128s，MySQL 25.782/29.281/28.516s）。对应 6 轮真实持锁取消/释放后健康重开回归全部通过，另有取消及解锁失败丢弃连接的 race 单测。日志 `startup-*-green1-*.log`、`migration-lock-*-green1-*.log`。独立只读审查无 Critical/Important。 |
| SQL catalog 刷新写放大、取消等待可能阻塞 Maven 1 秒预算 | 按 `{alias, repo}` 临时状态汇总已提交更新 ticket 与 maxAt；晚到 ticket 留到下一轮；完成后删除状态。用可取消 `semaphore.Weighted(1)`，`UpdatedAt` 取最大值不回退。 | `catalog-batch-green.log`：meta 41.905 秒、artifactcache 11.362 秒通过；inactive-state cleanup 断言及最终全量 Go/race 也已通过。 |
| 候选 catalog 事务正常提交后仍调用 Rollback，dbx hook 提前输出错误和堆栈 | 成功提交后跳过 rollback；刷新失败、取消和 commit 失败仍清理事务并保留错误链。 | 真 SQLite + hook 的 `catalog-tx-cleanup-red.log` 确认正常提交多一次 rollback；修复后四种清理路径的 race 连续 3 轮通过，包含在 `new-hot-fixes-green.log`。取消测试等待共享后台任务清理完成后再断言。 |
| Go 热缓存绕过通用读取合并，放大共享 SQL 负载 | 复用 `artifactcache.Store.Get` 的并发元数据快照、Touch、过期判断和独立对象 reader；自定义 metadata 写入后统一使旧 flight 失效。构造参数的覆盖优先级保持一致。 | 最终回归用只读源码 overlay 运行旧实现：128 个命中请求触发 tag/manifest/blob 各 128 次、Touch 57 次（`go-read-fanin-old-source-red.log`）；新实现各 1 次，写后快照失效及 body/header 独立性 race 连续 3 轮通过（Go 10.491s、artifactcache 3.283s、meta 清理 9.714s）。独立设计审查无 Critical/Important。 |
| Go 缓存复制 ETag 时直接按非 canonical map key 查找，丢失标准库返回的 `Etag` | 复用 `http.CanonicalHeaderKey` 规范化允许复制的 header 名称。 | 同一真实 HTTP 预热/SQLite 缓存回归先测出 ETag 为空；修复后保留 upstream ETag，并确认修改一个 caller 的 header 不影响其他 caller。 |

## 压力运行记录（非最终矩阵）

| 运行阶段 | 观察结果 | 判断 |
|---|---|---|
| 最早修复镜像，SQLite Maven JAR 热 512 / 45 秒 | 300 个请求超时，出现 4 次额外 origin 请求。 | 失败。 |
| repaired / batched 中间版本 | 热负载挂死改善，但仍观察到 4 次额外 origin 请求。 | 失败，不视为验收。 |
| bounded（仅 pull 批刷新与可取消 gate 版本）SQLite | 512 VU 热流量：4616 响应、0 HTTP 错误、0 extra origin。1024 VU：5820 完整响应、0 HTTP 错误、0 corrupt，但 1 次 extra origin。 | 512 轮符合该用例阈值；1024 轮未符合零额外 origin 阈值。两者均非当前最终镜像。 |
| 中间候选镜像 config ID `cb24a883de8d269a216a32f26a2efd33ae0702c7beb047078da7d5ebbad91530` 首轮 grouped 热流量 | 512 VU：1292 成功响应、1 次 unexpected EOF、7 次额外 origin；k6 checks/threshold 失败。 | 失败。二进制 SHA-256：`957e3eb43da99d4cee2e23ff5e580ed15d75b164c5192cf6bb0f13110b88c76f`。 |
| 中间候选镜像复测（同一 `cb24a883…`） | SQLite 512 VU hot：4579 有效响应、HTTP 错误 0、corrupt 0、额外 origin 0，通过。1024 VU：3265 有效响应、HTTP 错误 0、corrupt 0，但额外 origin 5（PyPI wheel 3、Maven POM 1、JAR 1），失败。 | 脚本首错停止，mixed-method 用例未完成。bounded 1024 的 1 次额外 origin 已确认来自 PyPI wheel。 |

候选提交 `b11dff8` 使用独立新卷完成 SQLite 全部 16 项：13 个冷对象各 512 请求全过；热 512 / 1024 分别有 10,041 / 11,363 个有效响应，HTTP 错误、正文损坏、失败检查与额外回源均为零；OCI 混合方法也通过。npm/PyPI 实际跟链验包成功，负载后健康状态 200，catalog 两项只读审计均无差异（`final-sqlite-*`）。

同候选 PostgreSQL/S3 双副本的 13 项冷请求全部通过，但热 512 项未通过：3,036 个有效响应、HTTP 错误和正文损坏为零，PyPI wheel、Maven POM/JAR 各出现 1 次额外回源（`final-postgres-hot-512.*`）。保留失败日志；未执行该候选的热 1024 和 MySQL 矩阵。检查发现候选事务封装成功提交后仍调用 Rollback，dbx 已提前输出错误堆栈；同时 Go 热路径仍逐请求读取和触碰 SQL 元数据。修复及全量 Go / race 回归已完成，SQLite 的通过不能替代修正后同镜像的完整 48 项重跑。

该候选本地镜像 inspect ID：`sha256:35470deb4b1e3565c2abbe9b7a1d166f09c0b7bf5987b52a458374cb2dd5e3e5`；二进制 SHA-256：`7e6215309102cb2b2ca29ddb5df705e362ad9705a5dcc9fe72c540f30d5cca8b`。已核对实际运行的 SQLite 与两个 PostgreSQL 副本二进制；源码 464 项指纹和镜像留存，未覆盖旧证据。

首轮 grouped 失败时 `/proc`、pressure、I/O 有明显活动，full 平均约 10–41%；Docker stats 也有高于 CPU 限额读数和长等待。证据不足以归因于环境。

另有早期 SQLite 修复镜像对 13 个冷对象各 512 请求全绿（`sqlite-cold-verification.json`）；该镜像不是最终 source，不能替代当前矩阵。元数据升级跟链与其他 targeted regression 也仅证明对应场景。

## 待补结果与限制

- 最终镜像的 48 项完整矩阵：**待执行 / 待填**；SQLite、PostgreSQL、MySQL 最终汇总及失败项明细：**待填**。
- 提交 `b11dff8` 的全量 Go：39 个 package 通过、9 个无测试文件、零失败（`go-full-final.log`）；全部生态、缓存与元数据 race 检查通过（`go-race-final.log`）。整仓 49 项 strict lint 零问题（`lint-final.log`，36.159s）；辅助函数拆分后的真实持锁取消/健康重开回归各数据库再连续 3 次通过（`migration-lock-final-postgres.log` 4.139s、MySQL 4.367s）。后续修复需要再验证。
- PostgreSQL 热流量后的 Go 读合并、ETag 与事务清理修复已通过当前源码全量 Go 和 race 检查（Go 29.222s、Maven 37.021s、npm 37.891s、PyPI 33.508s、meta 25.324s）；整仓 49 项 strict lint 零问题（31.835s），下一候选尚未构建；`b11dff8` 的部分通过不能作为后续候选最终验收。完整 48 项需要同镜像重跑。
- 旧 SQLite 测试数据的只读审计发现 3 个 PyPI repository 总数少于 canonical pull 合计，上游合计与 repository 合计一致（`sqlite-catalog-before-final.json`）；最终矩阵将使用全新专用数据卷，旧卷保留，不先修改历史计数。
- 汇总时填写每组实际镜像 digest、完成用例数、响应/错误与 origin 计数、容器 OOM/restart 及资源观测。报告工具成功退出不等同阈值通过。
- 此夹具使用内部网络、合成包和固定负载；不覆盖公网行为、真实大型归档、持续峰值采样，也不等同生产容量 SLA。

**最终结论：待最终同镜像复测、48 项矩阵及完整检查后填写。当前验收未通过。**
