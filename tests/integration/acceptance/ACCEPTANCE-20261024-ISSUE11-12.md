# RegiMux 1.3.8 验收报告

**结论：通过。** 同一冻结源码与候选完成全部验收：74/74 并发场景、完整 S3 磁盘 L1、双库 canonical 审计、六生态 Range 317/317，以及全仓静态和竞态检查。最终续跑整体退出码为 0；原失败记录保留。发行镜像及资产由提交后的独立发布流程构建和核验，不在本报告提前宣称已发布。

## 实现摘要

- 新增仅适用于 S3 object store 的可选磁盘 L1，默认关闭；在非 S3 driver 上开启会明确拒绝。
- 复用 Hashicorp LRU、gofrs/flock 与 djherbis/stream，提供有容量、条目、对象大小、并发 fill 和 TTL 限制的持久目录及共享流式读取。
- 完整长度和摘要验证后才发布缓存；取消、半途断流或未验证对象不发布。目录锁、重启索引、清理失效和关闭生命周期已有回归覆盖。
- OCI 首次缓存打开遇受控可恢复错误且调用方上下文仍有效时，最多 4 个并发回退健康上游；保留仓库归属、鉴权、长度、摘要和 Range 校验。
- 对缓存对象打开等待、调用者取消、body idle 与续传分别处理，保留调用者预算、Range/If-Range 和表示身份验证；响应仍流式交付。
- 修复固定长度消费者可能忽略“最后 n>0 字节附带错误”的完整性问题：坏尾块在长度完成前被拒绝，错误终态保持；零长错误摘要在构造时拒绝。摘要校验不依赖额外 EOF，正确 body 保持流式。

## 冻结身份与验证环境

| 项目 | 实际身份 |
| --- | --- |
| 源码冻结 | 1124 文件，其中 952 个 Go 文件 |
| Source manifest SHA-256 | `58b8e5aba8da94ed4912d247efeca583bd3d2e5c4b5936cf0826c48eb3cc3ebe` |
| 候选镜像 | `sha256:34feb5085fbdff2828603af1e707c56c7c059a768c6e87b1b5364eac0d20ad5c` |
| Candidate evidence SHA-256 | `5b05c942e500602d21bc0cedab4ec8152f1c7978d0e5a3aa1ae0f3c206a5f1c8` |
| 工具链 | Go 1.27.3 / Linux amd64 |
| 依赖身份 | 完整模块图 364 项，实际链接清单 179 项 |

冻结后恢复验收期间，产品源码 1124 文件及原 65 项 helper 均保持原哈希。提交前另移除 Python 测试辅助文件 test_s3_proxy_support.py 末尾多余空行，所有 AST 节点及其位置完全相同，生产 Go 源码未变；相关容器协议测试已复验，基础代理与 L1 代理各 6 项均通过。最终提交清单记录该格式修正及本报告。新增 suppression 命中 0，`.golangci.yml` 未改；没有放宽资源、负载或超时预算。

## 全仓与静态检查

| 检查 | 实际结果 |
| --- | --- |
| 根模块全仓 / build 模块 | 51 / 1 个包结果，均 exit 0 |
| 广覆盖 / 磁盘缓存 / 流边界竞态 | 25 + 3 + 2 个包结果，均 exit 0 |
| golangci-lint | 49 条规则，0 issues，exit 0 |
| main govulncheck | reachable 0、imported 0、module-only 1，exit 0 |
| build govulncheck | 未发现漏洞，exit 0 |

包结果数包括日志明确标注的无测试或无匹配测试包。唯一 module-only 项为 `GO-2026-5932`，关联 `golang.org/x/crypto v0.58.0` 的未维护 OpenPGP 包；保存的扫描记录没有 fixed 版本。不能将其写成“所有层级漏洞均为 0”。

## 真实客户端

- uv 0.11.23 在 8 MiB 中断后，由代理发起 bytes=8388608-75545684 的 206 请求，完整源字节合计 75,545,685，未重新下载整个 wheel；安装后的 10 个原始文件逐项校验。没有执行该 wheel 的包代码。
- 完整 PyPI/pip 流程通过，原始 wheel 长度和 SHA-256 均匹配固定制品。
- Maven 真实客户端通过 404 回退成功、缓存正文、HEAD、全 404、404 与 503 混合及终止性 401 六个场景；并发 Maven POM/JAR 另包含在 74 组矩阵内。

## #11：公开 wheel 对照与结论边界

固定 wheel 为 **75,545,685 字节**，SHA-256 为 `65f20c541a499f08c36ac3bfdafce350ca8c7cc23488c1faf4d9b9524b547156`。
旧版 1.3.7 与候选分别在 local、S3 上进行四组控制；warm/origin 均覆盖并发 **1/8/32**，总预算 **90 秒**、idle **10 秒**，完整长度与 SHA 校验错误数均为 0。
四组 HEAD 200、Range 206、不可满足 Range 416 均通过；1 秒调用者总预算负控按预期超时，未记录 OOM 或重启。
**#11 原始外部故障的唯一原因仍未确定。** 健康旧版在相同预算下也通过，不能把该对照写成已证明某个唯一外部根因。

## #12：S3 慢开控制

两组使用 2.2 秒 object-open 延迟与 5 秒调用者预算。
旧版返回 **500 / 2.026429700 秒**，origin fallback=0；候选完整 GET fallback 返回 **200 / 3.219703400 秒**，Range fallback 返回 **206 / 3.219491651 秒**，HEAD 200、不可满足 Range 416 也通过。
旧版 blob 为 1228 字节、候选为 1226 字节，分别按各自 SHA 校验；它们不是同一 blob 的严格耗时对照。

## 常规 48 + distinct 26 组

49g reader 实际 exit 0、accepted=true、errors=[]，接纳 **74/74**；合计 **309014 HTTP 请求、604444 checks 通过**。
所有报告 HTTP 失败率 0、checks 失败 0、corrupt 0、阈值失败 0、client exit 0。HTTP rate 的 `values.fails` 是 false 样本计数，没有误作 HTTP 错误数。

| 范围 / backend | 组数 | HTTP 请求 | checks 通过 / 失败 | 跨场景最高 p95 / max（ms） |
| --- | ---: | ---: | ---: | ---: |
| 常规 MySQL | 16 | 27761 | 55466 / 0 | 11842.264 / 12280.788 |
| 常规 SQLite | 16 | 166671 | 333286 / 0 | 640.094 / 853.623 |
| 常规 PostgreSQL | 16 | 87906 | 175756 / 0 | 5223.473 / 32181.176 |
| distinct PostgreSQL | 13 | 13338 | 19968 / 0 | 16078.515 / 17587.457 |
| distinct MySQL | 13 | 13338 | 19968 / 0 | 52479.662 / 52921.959 |

MySQL distinct Go-info 的原始最大延迟为 **52921.959211 ms**，p95 为 **52479.662322 ms**；这些是观测值，**没有宣称低延迟 SLO 达成**。
常规 hot-1024：MySQL S3 object GET 211→211，PostgreSQL 144→144；HEAD 分别增加 2059 / 6710，结论仅为 object GET 增量 0。

## 资源与 settled 状态

常规应用限制 1 GiB / GOMEMLIMIT 768 MiB；distinct 为 512 MiB / 384 MiB。下表每列取对应 backend 各应用实例的最大观测值，RSS/HWM 为应用进程 KiB（Linux /proc 原文标注 kB），cgroup 为字节。

| 范围 / backend | RSS / HWM KiB | cgroup current / peak bytes | settled 窗口秒 |
| --- | ---: | ---: | ---: |
| 常规 MySQL | 175480 / 193580 | 215814144 / 235622400 | 95.6879084 |
| 常规 SQLite | 238708 / 254772 | 287580160 / 323948544 | 95.3409909 |
| 常规 PostgreSQL | 179816 / 235628 | 224755712 / 282906624 | 95.7096369 |
| distinct PostgreSQL | 188572 / 198800 | 228749312 / 242102272 | 95.6916270 |
| distinct MySQL | 184932 / 198288 | 225140736 / 240652288 | 95.8315963 |

全部 settled 应用健康 200，probe/profile 一致；服务 running、无 OOM、无重启，cgroup memory events 为 0，目录聚合审计及 stop exit 均为 0。逐实例原值见独立 normal48/distinct26 facts 与原始 jsonl。

## 完整 L1f 实测

- 8 MiB 主动中断不发布 L1，fill reservation 收敛；完整 seed 按固定大小与 SHA 验证。
- 32 并发 warm 与 32 并发重启后读取全部 200、完整大小和 SHA 正确，两个窗口 S3 object GET 均 **1→1**。
- 重启前未注册的访问 counter 按 0 计；读取后严格要求 counter 存在、get/hit 增长至 32，所有 gauges 仍严格要求存在。
- HEAD、bounded/suffix Range、416、匹配/不匹配 If-Range 与 HEAD+Range 均通过，S3 GET 增量 0。
- 清理实际成功 DELETE=1，L1 entries=0、bytes=0；随后完整 refill 使 S3 object GET **1→2**、entries=1、bytes=75,545,685。
- `l1-wheel49f-evidence/runner.exit=0`、`stop.exit=0`，最终 audit metrics 与容器 inspect 分文件保存；应用状态和停止验证通过。

## 验收恢复来源

临时 reader 曾把合法 S3 before/after/audit sidecar 当作主报告；恢复版本固定复用严格选择器并保留原错误输出。临时 L1 helper 的空 IPAM 行、重启前惰性 counter、测试 S3 卷批量分配耗尽和 audit 被 inspect 覆盖也分别形成保留的失败证据，随后以新增 helper / 隔离目录修正。S3 仅将单副本分配批量 7→1，其余默认及 8×128 MiB、CPU/RAM/超时保持。原失败 1、reader 2 和日志没有改成成功；产品冻结源码及原 65 项 helper 未变。已实际通过的 74 组及完整 L1f 按原始输入哈希复核后复用，严格 reader 与 canonical 实际通过后，Range 首次因测试端口误写为 5442 失败；独立 RangeH 改用实际监听的 5432，保留原 317 项断言和预算后真实全部通过。所有续跑结果均从实际执行和原始记录获得。

## 最终验收结果

| 阶段 | 实际结果 | 结论 |
| --- | --- | --- |
| 严格 reader | exit 0、accepted=true、74/74、errors=[]，源码和原始结果哈希一致 | 已通过 |
| canonical 双库 | PostgreSQL / MySQL 各九行完全匹配，total=5632 / 5632；query/audit/stop/总 exit 均 0 | 已实测通过 |
| 六生态 Range / 断点续传 | 73 次 HTTP 请求，317/317 检查通过，失败 0；冷缓存发布、HEAD/Range/If-Range 边界均验证 | range49h runner/client exit 0 |
| 完整验收续跑 | final49h-all-acceptance.exit=0；原 49/49b…49g 失败输出均未覆盖 | 全部已授权验收门禁通过 |

## 关键证据路径

以下路径均相对于 `tmp/hotcache-20261024-49/`，最终状态以原始结果为准。

- 身份 / Go：`source-freeze-49.json`、`gate49-candidate/evidence.json`、`post-broad49-source-binding.json`、`go-full-49.log`、`transfer-race-49.log`、`lint49-validation49a.log`、`govuln-*-49a.{log,json}`。
- 对照 / 负载：`controls49-independent/wheel-controls49-summary.json`、`oci-slow-{old13749,fixed49}/probe.log`、`matrix49-*`、`memory-mem49-{postgres,mysql}-*.json`、`report-preparation/{normal48,distinct26}-facts.json`。
- L1 / 恢复：`l1-wheel49f-evidence/{warm,restart,cleanup-observation,s3-final}.json`、`aggregation49b/recovery-binding.json`、`aggregation49b/continuation49g-provenance.json`、`aggregation49b/continuation49h-provenance.json`、各轮保留的失败 exit/log。
- 最终：`final49g-v6-machine-summary.json`、`final49g-reader.exit`、`canonical-audit49/{postgres,mysql}-observed.txt`、`canonical-audit49/exit`、`range49h-evidence/probe-summary.json`、`range49h-runner.exit`、`final49h-all-acceptance.exit`。
