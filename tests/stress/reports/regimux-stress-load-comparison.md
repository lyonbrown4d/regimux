# RegiMux k6 Load Comparison

- generated_at: 2026-10-15T05:54:31.308Z
- profile: load
- base_url: http://regimux:8080
- stores: sqlite, mysql, postgres

## Sources

| metadata store | report | generated_at |
| --- | --- | --- |
| sqlite | /stress/reports/regimux-stress-sqlite-load.json | 2026-10-15T05:33:20.079Z |
| mysql | /stress/reports/regimux-stress-mysql-load.json | 2026-10-15T05:45:15.106Z |
| postgres | /stress/reports/regimux-stress-postgres-load.json | 2026-10-15T05:54:20.079Z |

## Overall

| metadata store | requests | req/s | failed | avg | p95 | p99 | data received |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| sqlite | 210833 | 477.17 | 0.00% | 23.14 ms | 15.50 ms | 661.64 ms | 2.22 GiB |
| mysql | 339501 | 785.41 | 0.00% | 14.37 ms | 9.30 ms | 386.77 ms | 2.54 GiB |
| postgres | 369942 | 860.60 | 0.00% | 13.06 ms | 14.81 ms | 351.54 ms | 4.20 GiB |

## RegiMux Container Resources

| metadata store | samples | interval |
| --- | ---: | ---: |
| sqlite | 203 | 1000 ms |
| mysql | 182 | 1000 ms |
| postgres | 167 | 1000 ms |

### Gauges

| metadata store | metric | avg | p95 | peak |
| --- | --- | ---: | ---: | ---: |
| sqlite | CPU | 18.77% | 68.49% | 202.58% |
| sqlite | memory usage | 68.92 MiB | 123.40 MiB | 127.00 MiB |
| sqlite | memory utilization | 0.22% | 0.40% | 0.41% |
| sqlite | PIDs | 15.90 | 21 | 21 |
| mysql | CPU | 26.65% | 68.76% | 347.40% |
| mysql | memory usage | 52.51 MiB | 101.10 MiB | 105.60 MiB |
| mysql | memory utilization | 0.17% | 0.33% | 0.34% |
| mysql | PIDs | 17.21 | 25 | 25 |
| postgres | CPU | 34.44% | 68.12% | 361.17% |
| postgres | memory usage | 55.18 MiB | 104.60 MiB | 108.60 MiB |
| postgres | memory utilization | 0.18% | 0.34% | 0.35% |
| postgres | PIDs | 16.39 | 19 | 19 |

### Cumulative Deltas and Status

| metadata store | network received | network sent | block read | block written | restarts | OOM observed | OOM final |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| sqlite | 242.23 MiB | 2.26 GiB | 10.49 MiB | 2.66 GiB | 0 | no | no |
| mysql | 423.43 MiB | 2.62 GiB | 0 B | 3.05 GiB | 0 | no | no |
| postgres | 450.13 MiB | 4.27 GiB | 0 B | 5.00 GiB | 0 | no | no |

## Scenario p95

| name | sqlite | mysql | postgres |
| --- | ---: | ---: | ---: |
| container_blob_cold | 2123.25 ms | 1914.17 ms | 1575.59 ms |
| container_blob_hot | 1077.70 ms | 380.85 ms | 306.07 ms |
| container_blob_same_digest_concurrent | 2078.78 ms | 600.55 ms | 605.23 ms |
| container_manifest_cold | 324.80 ms | 302.73 ms | 274.79 ms |
| container_manifest_hot | 171.63 ms | 9.42 ms | 8.84 ms |
| container_multi_repo_mixed | 863.52 ms | 1262.50 ms | 451.11 ms |
| container_referrers_tags | 2.79 ms | 0.85 ms | 0.77 ms |
| health_baseline | 0.46 ms | 0.29 ms | 0.28 ms |
| maven_pom_hot | 337.25 ms | 448.37 ms | 221.41 ms |
| maven_release_hot | 701.72 ms | 593.58 ms | 218.23 ms |
| mixed_ecosystems | 1177.32 ms | 572.15 ms | 519.57 ms |
| npm_metadata_hot | 269.84 ms | 246.57 ms | 198.09 ms |
| npm_tarball_hot | 468.24 ms | 286.37 ms | 216.41 ms |
| pypi_simple_hot | 396.46 ms | 864.97 ms | 212.06 ms |
| pypi_wheel_hot | 395.58 ms | 1578.25 ms | 214.52 ms |

## Scenario Throughput

| name | sqlite req/s | mysql req/s | postgres req/s |
| --- | ---: | ---: | ---: |
| container_blob_cold | 0.01 | 0.01 | 0.01 |
| container_blob_hot | 1.16 | 2.12 | 3.10 |
| container_blob_same_digest_concurrent | 1.29 | 2.60 | 2.93 |
| container_manifest_cold | 0.00 | 0.00 | 0.00 |
| container_manifest_hot | 15.67 | 37.25 | 38.11 |
| container_multi_repo_mixed | 14.50 | 11.52 | 31.12 |
| container_referrers_tags | 293.69 | 543.30 | 568.67 |
| health_baseline | 123.90 | 160.16 | 167.05 |
| maven_pom_hot | 4.12 | 3.37 | 5.67 |
| maven_release_hot | 2.79 | 1.88 | 6.06 |
| mixed_ecosystems | 6.06 | 11.98 | 13.30 |
| npm_metadata_hot | 4.70 | 4.20 | 6.59 |
| npm_tarball_hot | 3.30 | 3.66 | 5.93 |
| pypi_simple_hot | 3.04 | 2.33 | 6.12 |
| pypi_wheel_hot | 2.92 | 1.04 | 5.93 |

## Endpoint p95

| name | sqlite | mysql | postgres |
| --- | ---: | ---: | ---: |
| container_blob_cold | 2330.17 ms | 1524.61 ms | 1717.19 ms |
| container_blob_cold_manifest | 258.42 ms | 1873.88 ms | 296.89 ms |
| container_blob_hot | 1077.70 ms | 380.85 ms | 306.07 ms |
| container_blob_same_digest_concurrent | 2078.78 ms | 600.55 ms | 605.23 ms |
| container_manifest_cold | 324.80 ms | 302.73 ms | 274.79 ms |
| container_manifest_hot | 171.63 ms | 9.42 ms | 8.84 ms |
| container_multi_repo_blob | 1446.71 ms | 1676.33 ms | 631.31 ms |
| container_multi_repo_blob_manifest | 496.95 ms | 420.75 ms | 151.82 ms |
| container_multi_repo_manifest | 672.06 ms | 1388.01 ms | 307.51 ms |
| container_multi_repo_tags | 5.22 ms | 0.72 ms | 0.54 ms |
| container_referrers | 2.77 ms | 0.83 ms | 0.75 ms |
| container_tags | 2.80 ms | 0.87 ms | 0.78 ms |
| health_baseline | 0.46 ms | 0.29 ms | 0.28 ms |
| maven_pom_hot | 337.25 ms | 448.37 ms | 221.41 ms |
| maven_release_hot | 701.72 ms | 593.58 ms | 218.23 ms |
| mixed_container_blob | 1751.18 ms | 750.77 ms | 700.99 ms |
| mixed_container_manifest | 727.07 ms | 399.75 ms | 358.29 ms |
| mixed_maven_pom | 998.68 ms | 391.52 ms | 414.83 ms |
| mixed_maven_release | 1117.17 ms | 413.35 ms | 424.15 ms |
| mixed_npm_metadata | 1038.76 ms | 455.45 ms | 416.12 ms |
| mixed_npm_tarball | 1006.29 ms | 433.22 ms | 398.92 ms |
| mixed_pypi_simple | 1190.36 ms | 435.34 ms | 416.30 ms |
| mixed_pypi_wheel | 838.74 ms | 454.51 ms | 455.81 ms |
| npm_metadata_hot | 269.84 ms | 246.57 ms | 198.09 ms |
| npm_tarball_hot | 468.24 ms | 286.37 ms | 216.41 ms |
| pypi_simple_hot | 396.46 ms | 864.97 ms | 212.06 ms |
| pypi_wheel_hot | 395.58 ms | 1578.25 ms | 214.52 ms |

## Endpoint Throughput

| name | sqlite req/s | mysql req/s | postgres req/s |
| --- | ---: | ---: | ---: |
| container_blob_cold | 0.00 | 0.00 | 0.00 |
| container_blob_cold_manifest | 0.00 | 0.00 | 0.00 |
| container_blob_hot | 1.16 | 2.12 | 3.10 |
| container_blob_same_digest_concurrent | 1.29 | 2.60 | 2.93 |
| container_manifest_cold | 0.00 | 0.00 | 0.00 |
| container_manifest_hot | 15.67 | 37.25 | 38.11 |
| container_multi_repo_blob | 2.92 | 2.31 | 6.24 |
| container_multi_repo_blob_manifest | 5.83 | 4.62 | 12.48 |
| container_multi_repo_manifest | 2.89 | 2.31 | 6.21 |
| container_multi_repo_tags | 2.86 | 2.28 | 6.19 |
| container_referrers | 146.85 | 271.65 | 284.34 |
| container_tags | 146.84 | 271.65 | 284.33 |
| health_baseline | 123.90 | 160.16 | 167.05 |
| maven_pom_hot | 4.12 | 3.37 | 5.67 |
| maven_release_hot | 2.79 | 1.88 | 6.06 |
| mixed_container_blob | 0.76 | 1.49 | 1.67 |
| mixed_container_manifest | 0.76 | 1.49 | 1.67 |
| mixed_maven_pom | 0.74 | 1.52 | 1.68 |
| mixed_maven_release | 0.74 | 1.51 | 1.67 |
| mixed_npm_metadata | 0.78 | 1.49 | 1.64 |
| mixed_npm_tarball | 0.77 | 1.50 | 1.64 |
| mixed_pypi_simple | 0.76 | 1.49 | 1.66 |
| mixed_pypi_wheel | 0.76 | 1.50 | 1.67 |
| npm_metadata_hot | 4.70 | 4.20 | 6.59 |
| npm_tarball_hot | 3.30 | 3.66 | 5.93 |
| pypi_simple_hot | 3.04 | 2.33 | 6.12 |
| pypi_wheel_hot | 2.92 | 1.04 | 5.93 |

## Notes

- Compare reports generated with the same `REGIMUX_STRESS_PROFILE`; mixed profiles are shown as `mixed`.
- Cold scenario rows are short shared-iteration baselines; sustained throughput comparisons should focus on hot and mixed scenarios.
- Resource gauges use average, nearest-rank p95, and peak; cumulative Docker counters use reset-aware deltas.
- JSON output contains the same store, resource, scenario, and endpoint tables for CI trend storage.

