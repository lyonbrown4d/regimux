# 容器真实验收

本目录使用隔离的 Compose 项目 `regimux-acceptance-20261015`，连接真实 PostgreSQL、Redis 和 SeaweedFS S3 服务。客户端运行在独立容器中；Docker 拉取通过专用 DinD 服务完成。凭据仅用于本项目的临时验收数据，服务不发布宿主机端口。

`restricted-mirror` 对 Maven 返回 503，对 OCI 返回明确的公开镜像白名单 403；后续候选仍然是公网真实仓库。这样可以确定性覆盖失败切换，同时保留真实包管理器协议交互。

## 构建与启动

以下命令从仓库根目录执行。宿主机只需要 Git、Docker 和 Compose，Go、Python、uv、Maven、npm 均在容器内运行。

1. 将待验收提交通过 `git archive` 导出，复制到 Go 构建容器的 Linux 数据卷，再执行 `go build -p 1 -ldflags="-s -w" -o /tmp/regimuxd ./cmd/regimuxd`。链接参数沿用本仓库 GoReleaser 的调试符号设置。推荐 `GOMEMLIMIT=768MiB`、`GOMAXPROCS=2`，并为模块缓存和编译缓存使用独立命名卷。Windows 上避免直接在挂载目录中编译大型依赖。
2. 将 Linux 二进制复制到 `tmp/container-acceptance/bin/regimuxd`，构建服务和工具镜像：

```powershell
docker build -t regimux-acceptance:22ee8d2 -f tests/integration/acceptance/runtime.Dockerfile tmp/container-acceptance/bin
docker build -t regimux-acceptance-tools:22ee8d2 -f tests/integration/acceptance/tools.Dockerfile tests/integration/acceptance
docker compose -f tests/integration/acceptance/compose.yml up -d redis postgres s3 restricted-mirror dind
docker compose -f tests/integration/acceptance/compose.yml run --rm --no-deps tools python /scripts/bootstrap_live.py
docker compose -f tests/integration/acceptance/compose.yml up -d --wait regimux
docker compose -f tests/integration/acceptance/compose.yml up -d --wait regimux-peer
```

主实例先启动并完成数据库迁移，再启动第二个实例。工具依赖由 `uv.lock` 固定；容器里的 `uv sync --frozen` 创建环境。
重新构建其他提交时，将服务镜像标记为对应提交，并设置 `REGIMUX_ACCEPTANCE_IMAGE` 后重新启动两个实例；工具镜像可以复用。

## 客户端与协议

```powershell
docker compose -f tests/integration/acceptance/compose.yml run --rm --no-deps maven-client
docker compose -f tests/integration/acceptance/compose.yml run --rm --no-deps oci-client
docker compose -f tests/integration/acceptance/compose.yml run --rm --no-deps tools sh /scripts/pypi-client.sh
docker compose -f tests/integration/acceptance/compose.yml run --rm --no-deps tools python /scripts/pypi-cold-range.py
docker compose -f tests/integration/acceptance/compose.yml run --rm --no-deps npm-client
docker compose -f tests/integration/acceptance/compose.yml run --rm --no-deps go-client
docker compose -f tests/integration/acceptance/compose.yml run --rm --no-deps tools python /scripts/dist-client.py
docker compose -f tests/integration/acceptance/compose.yml run --rm --no-deps tools python /scripts/replica-client.py
docker compose -f tests/integration/acceptance/compose.yml run --rm --no-deps tools pyright --project /scripts/pyproject.toml
```

- Maven：两个空本地仓库分别使用固定版本的 Maven Compiler Plugin 编译调用 Commons IO 的 Java 代码，校验生成的 class、POM、JAR、SHA-1 和服务端缓存命中。
- OCI：真实 `docker pull`，删除本地镜像标签后再次拉取，校验缓存命中。
- PyPI：两个独立 pip 缓存下载、`uv lock`、`uv sync --frozen`，校验 SHA-256、HEAD、Range、If-Range、416 和完整缓存内容。
- npm / Go：各两个客户端并发下载真实包；Go 校验 info、mod、zip 三类文件。
- dist：下载 Node 官方二进制和校验清单，验证 SHA-256 和再次命中缓存。
- 双实例：同时下载同一 Maven JAR，比较内容，再确认两个实例均命中共享缓存。

S3 故障降级场景先执行 `tools python /scripts/cache-outage.py prepare`，再停止本 Compose 项目的 `s3` 服务，执行 `tools python /scripts/cache-outage.py verify`。验证已缓存的 Maven/PyPI 内容仍可从健康上游返回、冷 Maven POM 下载也成功，并记录耗时；无论验证结果如何，都应在 `finally` 中重新启动本项目的 S3 服务。

`pypi-cold-range.py` 使用未缓存的 six 1.16.0 wheel 检查冷请求返回 206，SQL/S3 均未发布局部内容，再验证完整下载的 SHA-256 和 S3 缓存。重复执行时需重置验收环境，或用 `REGIMUX_COLD_PYPI_VERSION` 指定另一个未缓存版本。

## S3 整包清理

先完成 PyPI 客户端验收，生成真实 wheel 标识，再生成清理目录：

```powershell
docker compose -f tests/integration/acceptance/compose.yml run --rm --no-deps tools python /scripts/make_cleanup_catalog.py
docker compose -f tests/integration/acceptance/compose.yml run --rm --no-deps -e REGIMUX_ACCEPTANCE_CLEANUP_CASES_FILE=/reports/cleanup-cases.json -e REGIMUX_ACCEPTANCE_CLEANUP_SNAPSHOT=/reports/cleanup-manual.json tools python /scripts/cleanup_live.py
```

脚本只允许本 Compose 项目的数据库、bucket 和服务地址。它先通过代理下载六个生态的真实内容，记录 SQL 元数据和 S3 HEAD 快照，再把验收数据的访问时间调旧。Maven 保留组只刷新一个 POM，Go 保留组只刷新一个 info，以检查兄弟文件整体保留。额外插入一条明确标记的合成引用，用于验证跨生态共享 CAS 内容不会误删；真实包下载本身不依赖合成数据。

手动模式并发请求两个实例的 `POST /admin/scheduler/cleanup`，检查 202 JSON 响应，随后等待过期组元数据和独占 S3 对象实际删除、活跃组及共享对象保留，并重复触发检查幂等。202 仅表示受理，不能作为清理完成证据。

补充场景：

1. 叠加 `compose.manual.yml` 重建两个实例，再运行上述脚本，验证关闭自动调度后 API 仍能清理。
2. 恢复默认 Compose。运行脚本时设置 `REGIMUX_ACCEPTANCE_CLEANUP_TRIGGER=scheduled`，等待输出 `PREPARED`；随后叠加 `compose.cron.yml` 重建主实例，等待下一分钟的真实 cron 完成清理。此模式不调用清理 API。验收结束后恢复默认配置。

默认配置仍为每天 **Asia/Shanghai 03:00**；一分钟 cron 仅用于本目录的临时验收。关闭整个 scheduler 时只有进程内互斥；开启 scheduler 并使用 Redis 时提供跨实例任务锁。

## 记录与回收

运行日志、wheel 报告及清理快照保存在已忽略的 `tmp/container-acceptance/reports/`。Go 全仓回归也必须在构建容器内运行，例如 `go test -p 1 ./... -count=1 -timeout=180s`。首次冷编译应与真实客户端验收分阶段进行，避免挤占本地服务资源。

验收后使用本目录 Compose 的 `down -v` 回收本项目，并单独移除本次创建的构建容器/卷。不要执行全局 Docker prune，不要停止其他项目的服务。缓存对象删除与并发写入之间已有条件检查，但没有跨请求的线性化写入屏障；并发情况下允许缓存重新填充。
