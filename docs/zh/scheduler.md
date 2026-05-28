# 调度器

RegiMux 使用 `gocron` 执行后台任务，并通过 worker 池限制异步任务并发。

## 任务

当前任务包括：

- 缓存清理和对象容量控制
- 上游 mirror 探测
- 预测预拉取

配置 Redis 或 Valkey 后，调度任务可以使用分布式锁，避免多个副本重复执行同一类后台任务。

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

每个上游都可以配置 mirror 探测，并持久化 endpoint 健康状态：

```hcl
upstreams {
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

blob 拉取会优先选择健康且低延迟的 endpoint。失败 endpoint 会进入冷却窗口，内容不一致也会降低该 endpoint 的优先级。

## 预测预拉取

预拉取会基于拉取历史预测可能的后续 tag，然后通过和客户端拉取相同的缓存路径预热 manifest 和关联 blob。

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

运行记录和结果会存入元数据，并可在 Admin UI 中查看。

## Worker 池

```hcl
worker {
  probe_concurrency = 16
  prefetch_concurrency = 8
}
```

这些值应结合上游限流、对象存储带宽，以及本机 CPU/网络容量调整。

