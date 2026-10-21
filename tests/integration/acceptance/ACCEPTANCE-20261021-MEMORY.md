# 2026-10-21 内存修复发布前验收

## 范围与构建

覆盖合并后的 PR #8（Maven 镜像回退）和 issue #9（S3 小对象上传按 multipart part 大小提前分配内存），以及六个包生态共用的响应流、缓存和元数据内存边界。

候选源码以 `cec6d79abd9ad7bfbf04335afd792ea73876b4f1` 为基线，冻结清单为 `tmp/memory-20261021/source-freeze-12.json`（956 个文件，逐文件 SHA-256）。候选镜像 `regimux-memory:20261021` 的 ID 为 `sha256:42b3a9967fc10f17157cf2940b842ecf1b945bf314da7caf19120be6395561bd`。它由本机 Go 1.27.3 交叉编译为 Linux amd64、CGO 关闭，再经 UPX 5.2.1 `--best --lzma` 打包。该候选镜像来自提交前的源码，正式发布需从干净的发布提交重新构建并检查版本和提交身份。

未新增第三方依赖；复用已有 AWS SDK、semaphore、errgroup、LRU、JSON/XML/HTML 解析库。此次改动未新增 lint 抑制指令，也未降低 lint 规则。

## 已完成检查

Go 测试与 lint 在 Go 1.27.2 Linux 容器内运行：

| 检查 | 结果 | 本地证据 |
| --- | --- | --- |
| 根模块 `go test -ldflags="-s -w" -p 1 ./... -count=1 -timeout=360s` | 全部包通过 | `go-full-11.log/.exit` |
| 最后一次 lint 整理后的 Maven、PyPI、memory backend、meta 全包测试 | 通过 | `affected-12.log/.exit` |
| 现有 49 项 golangci-lint 检查 | 0 issues | `lint-freeze-12.log/.exit` |
| artifactstream、object 全包 `go test -race -v` | 通过 | `core-race-12.log/.exit` |
| 标准 HTTP 与 Fiber，已知/未知长度真实响应流 | 正文尚未完成时已收到响应头；正文完整、只关闭一次 | `core-race-12.log` |
| OCI 慢客户端取消、PyPI 上游正文阻塞的真实 HTTP 回归 | 通过 | `streaming-9.log/.exit` |
| S3 代理请求分块、签名头、扩展与 trailer 转发 | 2 项通过 | `proxy-unit-final.log` |
| 本机 GoReleaser 2.17.1 配置检查 | 通过 | `tmp/release-v1.3.6/native-goreleaser-check.log` |

全量测试对应第 11 次冻结；第 12 次只调整测试辅助函数与 Maven XML 错误文本的大小写。受影响包已完整重测，并完成第 12 次全仓 lint 和上述 race 检查。日志除特别注明外均位于 `tmp/memory-20261021/`。

S3 race 测试中，4 个并发 1 KiB 未知长度上传阻塞期间，存活堆新增 160,808 字节（原实现红测约 67.15 MB）。约 20 MiB multipart 输入的阻塞上传新增存活堆为 6,816 字节。这是测试进程的堆差值，不代表服务 RSS 上限。上传准入取消不读取源、取消后释放名额、失败 abort、spool 清理、调用方文件快照、不可变文件复用和起始偏移均通过回归。

## 真实容器验收

Maven 在独立 SQLite/local-object 容器中通过全部 6 个场景：404 后回退至 200、完整 POM 校验与缓存命中不回源、HEAD 长度和空正文、全部 404、后续 503 保留、401 不再回退。证据为 `maven8-acceptance.log/.exit`（退出码 0）。

S3 使用 PostgreSQL、Redis、SeaweedFS 和请求级流式代理；代理为每个 PUT 增加 100 ms 延迟。两个 RegiMux 副本各限制 512 MiB，设置 `GOMEMLIMIT=384MiB`。k6 同时请求 512 个不同的 Go ZIP 冷对象，每个正文完整校验 SHA-256，并逐键检查恰好一次上游 GET：

| 项目 | 结果 |
| --- | --- |
| 完成请求 | 512/512 |
| 内容或计数断言 | 1,536 次通过，0 失败 |
| HTTP 错误率、损坏正文 | 均为 0 |
| 客户端退出码 | 0 |
| 服务及依赖 OOM、重启 | 均为 0 |
| 主副本进程 RSS/HWM 最大观测 | 157,268 KiB / 157,268 KiB |
| 另一副本进程 RSS/HWM 最大观测 | 155,176 KiB / 155,176 KiB |
| 两副本 cgroup 内存峰值 | 198,021,120 / 195,670,016 字节 |

证据位于 `tmp/memory-20261021/core-pg/`：k6 JSON/日志、各阶段进程内存采样、服务日志和容器最终状态均保留。PID 1 为 docker-init，RSS/HWM 来自实际 regimuxd 进程；cgroup 数值单独记录。

该冷对象场景的 HTTP p95 为 70.58 秒、最大 71.73 秒（含 fixture 准备请求）。没有设置延迟通过阈值，因此上述结果只证明正确性和本场景资源边界，不能据此宣称高并发尾延迟已优化。运行中保留的 goroutine 样本显示部分请求等待目录元数据刷新。

## 尚待完成

SQLite/PostgreSQL/MySQL 的完整 48 组冷、热与混合并发矩阵，以及 PostgreSQL/MySQL 的完整 26 组不同冷对象内存矩阵，尚未在此候选镜像上全部完成。不能将本报告表述为完整 74 组验收通过。正式双架构镜像的提交身份、启动和公开标签校验，以及其他平台安装包验收另行记录。
