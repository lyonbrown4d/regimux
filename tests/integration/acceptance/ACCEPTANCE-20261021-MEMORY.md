# 2026-10-23 source 41 内存与流式修复验收

**预发布验收通过：source 41 Linux amd64 主候选的完整门禁均通过。** ARM 镜像 smoke 仍属于发布阶段待验证事项；本报告不表示候选已经公开发布。

## 验收对象与结果

- Source freeze：1054 个文件，SHA-256 92d8c52d616689995952ebe025254e604a033502e5e1604b401eb7601aadaf91，核验无差异。
- 候选镜像：regimux-memory:20261021-gate41，sha256:09699174aa424ce219de7a0d43801da7f561bf2e0e7009db8cc3e41f233fdb3a。
- 最终汇总 final41-v6-machine-summary.json 为 accepted=true，74/74 场景通过，errors 为空；51 包全量测试、覆盖 24 包相关场景的定向 race 检查、冻结源码 49 项 lint 均通过。source41 lint 抑制审计新增命中为 0，接受的 lint 规则配置未再改变。
- govuln source41 扫描未发现可达漏洞；另有 1 项未使用 OpenPGP 模块 advisory，故不表述为“无漏洞”。
- canonical audit 的 PostgreSQL 与 MySQL 严格核验均通过：各 9 行，5,632 项匹配。
- Maven #8 六项命名场景通过：404-to-200、cached-content、HEAD、all-404、404-and-503、terminal-401。
- 六生态 Range 验收通过：73 个 HTTP 请求、6 个 targets、317/317 项检查通过。证据绑定候选镜像。OCI 416 响应可包含 JSON 错误正文；该检查验证错误响应，不要求空正文。RANGE_INVALID 是测试 registry 的错误码，不是 OCI 通用错误码。
- Source 40 曾在 Range 上得到 315/317；一项是 Dist warm HEAD ETag 为空，source41 修复响应头 canonicalization 并通过新增回归；另一项是把 OCI 416 JSON 错误正文误判为失败，source41 按错误响应语义修正了预期。

## 并发矩阵

常规矩阵 48 组（MySQL、SQLite、PostgreSQL 各 16），distinct 矩阵 26 组（MySQL、PostgreSQL 各 13）。 常规冷缓存使用 512 VUs，热点使用 512/1024 VUs，混合负载使用 512 VUs；应用容器上限 1 GiB，GOMEMLIMIT 768 MiB。Distinct 每场景请求 512 个不同缓存键，使用 512 VUs、应用容器上限 512 MiB、GOMEMLIMIT 384 MiB。各组 threshold failures、HTTP failure rate 和 corrupt responses 均为 0。

| 组别 | 后端 | 场景 | 检查通过/失败 | 迭代 / HTTP 请求 | 最大 p95 / 场景最大延迟（ms） |
| --- | --- | ---: | ---: | ---: | ---: |
| 常规 | MySQL | 16 | 50,824 / 0 | 25,340 / 25,440 | 5,980.859 / 7,265.871 |
| 常规 | SQLite | 16 | 383,676 / 0 | 191,792 / 191,866 | 530.343 / 687.029 |
| 常规 | PostgreSQL | 16 | 261,800 / 0 | 130,828 / 130,928 | 1,386.988 / 1,550.960 |
| distinct | MySQL | 13 | 19,968 / 0 | 6,656 / 13,338 | 73,190.205 / 74,629.644 |
| distinct | PostgreSQL | 13 | 19,968 / 0 | 6,656 / 13,338 | 21,032.037 / 21,428.693 |

常规 fixture payload 参数为 128 KiB，distinct 为 1 KiB；各协议实际正文大小会因元数据或归档开销而不同，Go info 正文实际约 50 bytes。MySQL distinct 的 p95 达 73 秒以上，是本轮压测的观测值，不是低延迟 SLO，也不能解释为 SLO 通过。

## 稳定窗口与资源

每个 backend 的稳定采样窗口约 95.3 秒。最终服务均保持运行，OOM 和重启为 0。进程 RSS/HWM 用 KiB；cgroup current/peak 用 bytes。

| 矩阵 | 应用进程 RSS / HWM（KiB） | cgroup current / peak（bytes） |
| --- | --- | --- |
| 常规 MySQL 主 / peer | 188,280 / 230,876；219,300 / 226,816 | 227,635,200 / 313,352,192；258,539,520 / 321,368,064 |
| 常规 SQLite | 195,792 / 256,816 | 244,563,968 / 325,558,272 |
| 常规 PostgreSQL 主 / peer | 202,940 / 245,712；187,904 / 247,772 | 241,999,872 / 330,203,136；227,004,416 / 336,990,208 |
| distinct MySQL 主 / peer | 185,748 / 196,424；179,608 / 194,928 | 225,271,808 / 241,061,888；219,848,704 / 239,030,272 |
| distinct PostgreSQL 主 / peer | 175,144 / 191,408；175,384 / 195,016 | 213,340,160 / 235,126,784；215,052,288 / 237,543,424 |

这些是本次 Linux amd64 候选及给定负载下的测量值，不构成其他部署环境的内存上限保证。

## PyPI、wheel 与依赖边界

- 512 并发的 MySQL PyPI wheel 场景通过，1,536 项检查通过、HTTP failure rate 和 corrupt response 均为 0；场景用时约 44.4 秒。该并发负载不代表 512 个 75.5 MB wheel。
- 单独的严格 uv wheel 验收用时 15.0540732 秒。固定 macOS wheel 为 75,545,685 bytes，SHA-256 65f20c541a499f08c36ac3bfdafce350ca8c7cc23488c1faf4d9b9524b547156；从 8 MiB 位置以 Range 和强 ETag 续传并返回 206，10 个安装文件逐字节验证。未在 Linux 上执行其原生代码。
- 完整 PyPI 下载 10.76812496597995 秒，75,545,685 bytes，recoveries=1；客户端最大 RSS 25,756 KiB。regimux-demo 的 uv lock/sync/run import 与 pip 下载是单独验证。
- 主模块更新 13 个既有直接依赖、23 个既有间接依赖，并加入 go.opentelemetry.io/otel/log；Python lock 更新 7 项，build 模块更新 3 个既有间接依赖。没有任意升级主版本，也没有放宽 lint 规则或新增抑制。

## ORAS 与发布范围

ORAS 更新至 v2.6.4。私有 registry 与 Bearer realm 可使用相同主机名、不同端口；跨私有主机名的 token realm 或重定向会被拒绝，不向其他主机发送凭据。

本轮候选与运行验证覆盖 Linux amd64。ARM smoke 尚未运行，需在发布阶段单独验证。报告中的矩阵与 Range 数字均绑定 source41 freeze 和候选镜像；当前验收通过不表示已创建公开发布或发布标签。

## 证据索引

核心机器汇总为 final41-v6-machine-summary.json；canonical 对照记录位于 canonical-audit41/mysql-observed.txt 与 canonical-audit41/postgres-observed.txt；Range 详细请求与逐项断言位于 range41-evidence/http-transcript.jsonl 和 range41-evidence/probe-summary.json；新增 lint 抑制审计为 no-nolint41-audit.json。go-full-41.exit、transfer-race-41.exit、lint49-validation41a.exit、govuln41b-runner.exit、canonical41-runner.exit、range41-runner.exit 与 runtime41-complete.exit 均为 0。
