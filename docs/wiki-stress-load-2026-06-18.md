# Performance Stress Report Sync (2026-06-18)

## What changed / 更新内容

- Added mixed ecosystem and cold-vs-hot stress report results across all metadata stores.
- Generated artifacts:
  - `tests/stress/reports/regimux-stress-sqlite-load.json`
  - `tests/stress/reports/regimux-stress-mysql-load-v3.json`
  - `tests/stress/reports/regimux-stress-postgres-load-v1.json`
  - `tests/stress/reports/regimux-stress-load-comparison-20260618.md`
  - `tests/stress/reports/regimux-stress-load-comparison-20260618.json`
  - `tests/stress/reports/regimux-stress-load-wiki-cn-2026-06-18.md`

## Execution command / 执行命令

```sh
docker compose -f tests/integration/compose.yml --profile stress run --rm -e REGIMUX_STRESS_PROFILE=load -e REGIMUX_STRESS_META_STORE=sqlite regimuxd k6 run /stress/k6/regimux.js
task stress:mysql
task stress:postgres
docker compose -f tests/integration/compose.yml --profile stress run --rm --no-deps \
  -e REGIMUX_K6_COMPARE_REPORT_NAME=regimux-stress-load-comparison-20260618 \
  -e REGIMUX_K6_COMPARE_REPORTS="sqlite=/stress/reports/regimux-stress-sqlite-load.json,mysql=/stress/reports/regimux-stress-mysql-load-v3.json,postgres=/stress/reports/regimux-stress-postgres-load-v1.json" \
  k6 run --quiet /stress/k6/compare.js
```

## Overall throughput / 总体吞吐

| Metadata Store / 元数据存储 | Requests | Req/s | Failed / 失败率 | Avg / 均值(ms) | P95 | P99 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| sqlite | 347,080 | 842.91 | 0.00% | 13.49 | 10.14 | 418.69 |
| mysql | 350,520 | 818.75 | 0.00% | 13.97 | 0.70 | 0.94 |
| postgres | 336,324 | 818.57 | 0.00% | 14.24 | 0.74 | 310.31 |

## Cold (first-upstream hit) / 无缓存（首次上游）

| Metadata Store / 元数据存储 | Warmup count / 预热次数 | Warmup P95 | Warmup Max | container_manifest_cold P95 | container_blob_cold P95 | Blob cold req/s |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| sqlite | 31 | 4,565.30ms | 5,763.45ms | 1,200.62ms | 4,454.93ms | 0.01 |
| mysql | 30 | 4,809.53ms | 5,892.59ms | 2,706.67ms | 2,918.52ms | 0.01 |
| postgres | 30 | 3,454.15ms | 4,414.78ms | 761.93ms | 2,463.35ms | 0.01 |

## Hot / Mixed results / 热命中与混合生态

| Scene / 场景 | sqlite | mysql | postgres |
| --- | --- | --- | --- |
| container_manifest_hot Req/s / P95(ms) | 32.55 / 8.89 | 0.34 / 2,311.74 | 3.68 / 448.32 |
| container_blob_hot Req/s / P95(ms) | 2.94 / 286.94 | 0.09 / 9,452.00 | 0.49 / 1,359.93 |
| mixed_ecosystems Req/s / P95(ms) | 11.55 / 620.78 | 0.43 / 15,882.50 | 1.93 / 3,830.32 |
| container_referrers_tags Req/s / P95(ms) | 553.92 / 0.71 | 637.45 / 0.71 | 617.02 / 0.73 |

### Ecosystem hot endpoint / 生态热点接口

- npm metadata: 253.42ms（sqlite） / 2,350.32ms（mysql） / 665.57ms（postgres）
- npm tarball: 242.24ms（sqlite） / 2,308.65ms（mysql） / 644.42ms（postgres）
- pypi simple: 253.79ms（sqlite） / 2,459.90ms（mysql） / 588.93ms（postgres）
- pypi wheel: 260.21ms（sqlite） / 2,343.68ms（mysql） / 690.93ms（postgres）
- maven release: 253.17ms（sqlite） / 2,392.16ms（mysql） / 691.48ms（postgres）

## Conclusions / 结论

- sqlite has highest sustained overall throughput under this profile.
- mysql and postgres have similar overall req/s but show much heavier tail latency for mixed workload and hot non-container requests, likely because of upstream contention.
- container + non-container mixed traffic is the main stress point; failure rates still 0% in all environments.

## Copy to GitHub Wiki

- English wiki page: `https://github.com/lyonbrown4d/regimux/wiki/en-README`
- Chinese wiki page: `https://github.com/lyonbrown4d/regimux/wiki/zh-README`
- Suggested section title: `2026-06-18 真实环境压测（sqlite / MySQL / Postgres）`

