# RegiMux k6 Load Comparison

- generated_at: 2026-07-16T08:23:54.851Z
- profile: load
- base_url: http://regimux:8080
- stores: sqlite, mysql, postgres

## Sources

| metadata store | report | generated_at |
| --- | --- | --- |
| sqlite | /stress/reports/regimux-stress-sqlite-load.json | 2026-07-16T07:46:06.683Z |
| mysql | /stress/reports/regimux-stress-mysql-load.json | 2026-07-16T08:15:05.001Z |
| postgres | /stress/reports/regimux-stress-postgres-load.json | 2026-07-16T08:23:44.363Z |

## Overall

| metadata store | requests | req/s | failed | avg | p95 | p99 | data received |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| sqlite | 375591 | 920.01 | 0.00% | 12.42 ms | 9.69 ms | 419.94 ms | 3.47 GiB |
| mysql | 387192 | 948.98 | 0.00% | 12.04 ms | 11.38 ms | 339.58 ms | 4.39 GiB |
| postgres | 376904 | 926.78 | 0.00% | 12.35 ms | 10.31 ms | 381.65 ms | 3.83 GiB |

## RegiMux Container Resources

| metadata store | samples | interval |
| --- | ---: | ---: |
| sqlite | 184 | 1000 ms |
| mysql | 179 | 1000 ms |
| postgres | 176 | 1000 ms |

### Gauges

| metadata store | metric | avg | p95 | peak |
| --- | --- | ---: | ---: | ---: |
| sqlite | CPU | 30.46% | 65.10% | 352.73% |
| sqlite | memory usage | 52.42 MiB | 99.41 MiB | 100.00 MiB |
| sqlite | memory utilization | 0.17% | 0.32% | 0.32% |
| sqlite | PIDs | 15.63 | 19 | 23 |
| mysql | CPU | 34.50% | 64.55% | 350.69% |
| mysql | memory usage | 53.23 MiB | 104.30 MiB | 105.40 MiB |
| mysql | memory utilization | 0.17% | 0.34% | 0.34% |
| mysql | PIDs | 15.70 | 19 | 21 |
| postgres | CPU | 32.02% | 65.10% | 353.63% |
| postgres | memory usage | 52.10 MiB | 97.01 MiB | 97.66 MiB |
| postgres | memory utilization | 0.17% | 0.31% | 0.32% |
| postgres | PIDs | 15.35 | 18 | 18 |

### Cumulative Deltas and Status

| metadata store | network received | network sent | block read | block written | restarts | OOM observed | OOM final |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| sqlite | 464.44 MiB | 3.56 GiB | 0 B | 3.92 GiB | 0 | no | no |
| mysql | 474.93 MiB | 4.47 GiB | 0 B | 5.00 GiB | 0 | no | no |
| postgres | 461.58 MiB | 3.91 GiB | 0 B | 4.22 GiB | 0 | no | no |

## Scenario p95

| name | sqlite | mysql | postgres |
| --- | ---: | ---: | ---: |
| container_blob_cold | 2384.10 ms | 1941.27 ms | 2384.80 ms |
| container_blob_hot | 267.92 ms | 230.39 ms | 340.47 ms |
| container_blob_same_digest_concurrent | 706.58 ms | 474.91 ms | 537.95 ms |
| container_manifest_cold | 2359.41 ms | 508.90 ms | 546.45 ms |
| container_manifest_hot | 8.82 ms | 7.34 ms | 67.34 ms |
| container_multi_repo_mixed | 533.71 ms | 407.45 ms | 517.21 ms |
| container_referrers_tags | 0.71 ms | 0.72 ms | 0.71 ms |
| health_baseline | 0.26 ms | 0.25 ms | 0.25 ms |
| maven_release_hot | 263.92 ms | 197.56 ms | 247.41 ms |
| mixed_ecosystems | 681.40 ms | 466.87 ms | 580.47 ms |
| npm_metadata_hot | 232.56 ms | 183.33 ms | 220.06 ms |
| npm_tarball_hot | 248.39 ms | 188.14 ms | 244.55 ms |
| pypi_simple_hot | 252.06 ms | 187.14 ms | 241.93 ms |
| pypi_wheel_hot | 250.47 ms | 191.86 ms | 264.46 ms |

## Scenario Throughput

| name | sqlite req/s | mysql req/s | postgres req/s |
| --- | ---: | ---: | ---: |
| container_blob_cold | 0.01 | 0.01 | 0.01 |
| container_blob_hot | 3.03 | 3.79 | 3.09 |
| container_blob_same_digest_concurrent | 2.88 | 3.86 | 3.30 |
| container_manifest_cold | 0.00 | 0.00 | 0.00 |
| container_manifest_hot | 34.53 | 47.39 | 36.10 |
| container_multi_repo_mixed | 26.72 | 35.59 | 29.20 |
| container_referrers_tags | 630.45 | 622.79 | 626.27 |
| health_baseline | 185.05 | 188.21 | 188.04 |
| maven_release_hot | 5.01 | 6.45 | 5.72 |
| mixed_ecosystems | 10.96 | 14.08 | 12.14 |
| npm_metadata_hot | 5.58 | 6.41 | 6.22 |
| npm_tarball_hot | 5.09 | 6.87 | 5.71 |
| pypi_simple_hot | 5.34 | 6.72 | 5.65 |
| pypi_wheel_hot | 5.35 | 6.79 | 5.30 |

## Endpoint p95

| name | sqlite | mysql | postgres |
| --- | ---: | ---: | ---: |
| container_blob_cold | 2506.06 ms | 2046.22 ms | 2617.99 ms |
| container_blob_cold_manifest | 1233.17 ms | 957.49 ms | 285.25 ms |
| container_blob_hot | 267.92 ms | 230.39 ms | 340.47 ms |
| container_blob_same_digest_concurrent | 706.58 ms | 474.91 ms | 537.95 ms |
| container_manifest_cold | 2359.41 ms | 508.90 ms | 546.45 ms |
| container_manifest_hot | 8.82 ms | 7.34 ms | 67.34 ms |
| container_multi_repo_blob | 782.91 ms | 515.63 ms | 663.20 ms |
| container_multi_repo_blob_manifest | 214.66 ms | 144.51 ms | 181.23 ms |
| container_multi_repo_manifest | 460.93 ms | 237.71 ms | 359.61 ms |
| container_multi_repo_tags | 0.52 ms | 0.50 ms | 0.53 ms |
| container_referrers | 0.69 ms | 0.69 ms | 0.69 ms |
| container_tags | 0.73 ms | 0.73 ms | 0.73 ms |
| health_baseline | 0.26 ms | 0.25 ms | 0.25 ms |
| maven_release_hot | 263.92 ms | 197.56 ms | 247.41 ms |
| mixed_container_blob | 808.68 ms | 627.94 ms | 745.54 ms |
| mixed_container_manifest | 536.58 ms | 381.31 ms | 399.73 ms |
| mixed_maven_release | 489.22 ms | 442.39 ms | 479.85 ms |
| mixed_npm_metadata | 529.43 ms | 373.68 ms | 420.79 ms |
| mixed_npm_tarball | 525.26 ms | 402.14 ms | 470.46 ms |
| mixed_pypi_simple | 500.29 ms | 419.65 ms | 464.42 ms |
| mixed_pypi_wheel | 442.33 ms | 393.87 ms | 420.65 ms |
| npm_metadata_hot | 232.56 ms | 183.33 ms | 220.06 ms |
| npm_tarball_hot | 248.39 ms | 188.14 ms | 244.55 ms |
| pypi_simple_hot | 252.06 ms | 187.14 ms | 241.93 ms |
| pypi_wheel_hot | 250.47 ms | 191.86 ms | 264.46 ms |

## Endpoint Throughput

| name | sqlite req/s | mysql req/s | postgres req/s |
| --- | ---: | ---: | ---: |
| container_blob_cold | 0.00 | 0.00 | 0.00 |
| container_blob_cold_manifest | 0.00 | 0.00 | 0.00 |
| container_blob_hot | 3.03 | 3.79 | 3.09 |
| container_blob_same_digest_concurrent | 2.88 | 3.86 | 3.30 |
| container_manifest_cold | 0.00 | 0.00 | 0.00 |
| container_manifest_hot | 34.53 | 47.39 | 36.10 |
| container_multi_repo_blob | 5.36 | 7.10 | 5.85 |
| container_multi_repo_blob_manifest | 10.72 | 14.20 | 11.71 |
| container_multi_repo_manifest | 5.34 | 7.15 | 5.83 |
| container_multi_repo_tags | 5.30 | 7.14 | 5.80 |
| container_referrers | 315.22 | 311.40 | 313.14 |
| container_tags | 315.23 | 311.39 | 313.13 |
| health_baseline | 185.05 | 188.21 | 188.04 |
| maven_release_hot | 5.01 | 6.45 | 5.72 |
| mixed_container_blob | 1.57 | 2.02 | 1.73 |
| mixed_container_manifest | 1.58 | 2.02 | 1.72 |
| mixed_maven_release | 1.57 | 2.02 | 1.73 |
| mixed_npm_metadata | 1.56 | 1.99 | 1.73 |
| mixed_npm_tarball | 1.57 | 2.00 | 1.74 |
| mixed_pypi_simple | 1.56 | 2.01 | 1.73 |
| mixed_pypi_wheel | 1.56 | 2.02 | 1.75 |
| npm_metadata_hot | 5.58 | 6.41 | 6.22 |
| npm_tarball_hot | 5.09 | 6.87 | 5.71 |
| pypi_simple_hot | 5.34 | 6.72 | 5.65 |
| pypi_wheel_hot | 5.35 | 6.79 | 5.30 |

## Notes

- Compare reports generated with the same `REGIMUX_STRESS_PROFILE`; mixed profiles are shown as `mixed`.
- Cold scenario rows are short shared-iteration baselines; sustained throughput comparisons should focus on hot and mixed scenarios.
- Resource gauges use average, nearest-rank p95, and peak; cumulative Docker counters use reset-aware deltas.
- JSON output contains the same store, resource, scenario, and endpoint tables for CI trend storage.

