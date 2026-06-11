# 调度器

RegiMux 使用 `gocron` 执行依赖代理后台任务，并通过 worker 池限制异步任务并发。

## 任务

当前任务包括：

- 依赖缓存清理和对象容量控制
- runtime 声明的 `probe` job
- runtime 声明的 `prefetch` job
- runtime 声明的 `manifest_refresh` job（适用时仅刷新 manifest）

配置 Redis 或 Valkey 后，调度任务可以使用分布式锁，避免多个副本重复执行同一类后台任务。probe 任务也会把 endpoint 健康状态发布到 Redis/Valkey 热状态层，但 SQL 元数据仍是持久化事实来源。

调度器不持有具体生态的依赖 fetch 逻辑。生态模块通过 `dix` 注册 runtime，每个 runtime 通过 `JobProvider` 声明 `ecosystem.JobSpec`，调度器只把这些 spec 翻译成 `gocron` job。container、Go、npm、PyPI 和 Maven 后续增减生态专属后台任务时，不需要改调度器主流程。

## 清理

清理任务会删除超过 `scheduler.cleanup.unused_for` 未访问的缓存 blob 对象。

```hcl
scheduler {
  cleanup {
    enabled = true
    interval = "1h"
    unused_for = "168h"
    max_deletes = 1000
    max_bytes = 10737418240
    target_bytes = 8589934592
  }
}
```

设置 `max_bytes` 和 `target_bytes` 后，RegiMux 会按最近访问时间优先淘汰未受保护的 blob，直到对象缓存达到目标水位或触达扫描/删除限制。

## Mirror 探测

实现 `probe` 的 runtime 可以调度 mirror 健康检查，并持久化 endpoint 健康状态。container alias 会用它做基于延迟的 blob mirror 选择：

```hcl
container {
  hub {
    blob {
      mirror_policy = "latency"
      top_n = 3
      max_concurrent_attempts = 1
    }

    probe {
      enabled = true
      interval = "30s"
      timeout = "3s"
      cooldown = "2m"
      jitter = "5s"
    }
  }
}
```

container blob 拉取会优先选择健康且低延迟的 endpoint。失败 endpoint 会进入冷却窗口，内容不一致也会降低该 endpoint 的优先级。

Go、npm、PyPI 和 Maven alias 也可以启用同一套 endpoint 可达性探测：

```hcl
npm {
  default {
    registry = "https://registry.npmjs.org"
    mirrors = ["https://registry.npmmirror.com"]

    probe {
      enabled = true
      interval = "1m"
      timeout = "3s"
      cooldown = "2m"
      jitter = "10s"
    }
  }
}
```

endpoint 健康状态会写入 SQL 元数据；当 cache backend 是 Redis 或 Valkey 时，也会同步到由 endpoint Hash、alias 级 Set 和 ZSet 组成的热状态索引。依赖生态的 probe 记录会使用带生态前缀的元数据 alias，例如 `npm/default`，避免和 container alias 冲突。

## 预测预拉取

实现 `prefetch` 的 runtime 可以调度缓存预热。container prefetch 会基于拉取历史预测可能的后续 tag，然后通过和客户端拉取相同的缓存路径预热 manifest 和关联 blob。Go、npm、PyPI 和 Maven 当前实现的是 recent-pull rewarm：客户端请求过某个制品后，定时 prefetch 可以沿用对应生态 proxy 缓存路径刷新同一个制品。

```hcl
scheduler {
  prefetch {
    enabled = true
    interval = "30m"
    min_pull_count = 2
    max_candidates_per_repo = 3
    max_version_distance = 5
    max_bytes = 0
    max_tasks = 0
    max_repositories = 0
    failure_backoff = "1h"
    retry_window = "24h"
  }
}
```

运行记录和结果会存入元数据，并可在 Admin UI 中查看。依赖生态 prefetch 记录使用 `go/default`、`npm/default` 这类 scoped alias；npm/PyPI/Maven/Go 的版本预测会作为后续生态专属层继续迭代。

## Manifest 刷新

`manifest_refresh` 使用同一套预热管道，但只执行 manifest 刷新：会拉取 manifest 和索引 manifest 子 manifest，不会下载 blob。适合在低带宽场景下定期保持镜像元数据新鲜。

```hcl
scheduler {
  manifest_refresh {
    enabled = true
    interval = "30m"
    distributed = true

    ecosystems {
      container {
        interval = "10m"
      }

      go {
        enabled = false
      }
    }
  }
}
```

如果不配置 `ecosystems`，一个 manifest refresh job 会覆盖所有支持 prefetch 的 runtime。配置 `ecosystems` 后，每个 runtime 会按自己的有效配置注册独立 job；未指定字段继承上层 `manifest_refresh`，`enabled = false` 可关闭某个生态。

## Recent-Pull 刷新

用户请求面对的 service API 保持 cache-first。请求命中本地缓存时，包括 stale 元数据，service 会发布 `artifact.pulled`，而不是在请求链路里强制访问上游。scheduler 会把刷新意图写入元数据存储，并按 `(ecosystem, kind, alias, repository, reference, accept)` 在 `scheduler.refresh.window` 内去重。

```hcl
scheduler {
  refresh {
    enabled = true
    window = "10m"
    distributed = true
  }
}
```

默认窗口是 10 分钟。同一个制品在窗口内被拉取 100 次，也只会消费一次到期刷新意图。窗口到期并消费后，如果后续再次被拉取，才会创建下一次刷新意图。

## Worker 池

```hcl
worker {
  probe_concurrency = 16
  prefetch_concurrency = 8
}
```

这些值应结合上游限流、对象存储带宽，以及本机 CPU/网络容量调整。
