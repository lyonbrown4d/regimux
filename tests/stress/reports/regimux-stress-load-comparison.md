# RegiMux k6 Load Comparison

- generated_at: 2026-07-15T15:52:45.243Z
- profile: load
- base_url: http://regimux:8080
- stores: sqlite, mysql, postgres

## Sources

| metadata store | report | generated_at |
| --- | --- | --- |
| sqlite | /stress/reports/regimux-stress-sqlite-load.json | 2026-07-15T15:34:12.364Z |
| mysql | /stress/reports/regimux-stress-mysql-load.json | 2026-07-15T15:43:53.406Z |
| postgres | /stress/reports/regimux-stress-postgres-load.json | 2026-07-15T15:52:19.299Z |

## Overall

| metadata store | requests | req/s | failed | avg | p95 | p99 | data received |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| sqlite | 353247 | 866.02 | 0.00% | 13.23 ms | 13.57 ms | 386.43 ms | 3.85 GiB |
| mysql | 375160 | 916.99 | 0.00% | 12.45 ms | 16.41 ms | 327.46 ms | 4.48 GiB |
| postgres | 366169 | 893.29 | 0.00% | 12.82 ms | 10.73 ms | 336.54 ms | 4.12 GiB |

## Scenario p95

| name | sqlite | mysql | postgres |
| --- | ---: | ---: | ---: |
| container_blob_cold | 2511.67 ms | 2475.95 ms | 2545.48 ms |
| container_blob_hot | 245.01 ms | 228.53 ms | 348.82 ms |
| container_blob_same_digest_concurrent | 631.66 ms | 507.54 ms | 595.89 ms |
| container_manifest_cold | 2012.95 ms | 1610.97 ms | 509.69 ms |
| container_manifest_hot | 13.88 ms | 10.17 ms | 8.39 ms |
| container_multi_repo_mixed | 491.47 ms | 388.85 ms | 446.37 ms |
| container_referrers_tags | 0.78 ms | 0.74 ms | 0.80 ms |
| health_baseline | 0.28 ms | 0.28 ms | 0.26 ms |
| maven_release_hot | 228.32 ms | 193.77 ms | 207.28 ms |
| mixed_ecosystems | 572.52 ms | 467.20 ms | 831.21 ms |
| npm_metadata_hot | 228.16 ms | 195.78 ms | 273.14 ms |
| npm_tarball_hot | 229.51 ms | 197.57 ms | 197.99 ms |
| pypi_simple_hot | 232.80 ms | 225.22 ms | 207.52 ms |
| pypi_wheel_hot | 274.39 ms | 219.00 ms | 203.11 ms |

## Scenario Throughput

| name | sqlite req/s | mysql req/s | postgres req/s |
| --- | ---: | ---: | ---: |
| container_blob_cold | 0.01 | 0.01 | 0.01 |
| container_blob_hot | 3.39 | 3.86 | 2.93 |
| container_blob_same_digest_concurrent | 3.05 | 3.63 | 3.57 |
| container_manifest_cold | 0.00 | 0.00 | 0.00 |
| container_manifest_hot | 34.21 | 43.47 | 43.18 |
| container_multi_repo_mixed | 29.60 | 35.76 | 32.52 |
| container_referrers_tags | 573.68 | 603.67 | 584.19 |
| health_baseline | 181.08 | 179.26 | 183.64 |
| maven_release_hot | 5.85 | 6.91 | 6.97 |
| mixed_ecosystems | 12.81 | 14.56 | 9.59 |
| npm_metadata_hot | 5.52 | 6.25 | 5.63 |
| npm_tarball_hot | 5.90 | 6.84 | 7.10 |
| pypi_simple_hot | 5.77 | 6.36 | 6.90 |
| pypi_wheel_hot | 5.13 | 6.36 | 7.04 |

## Endpoint p95

| name | sqlite | mysql | postgres |
| --- | ---: | ---: | ---: |
| container_blob_cold | n/a | n/a | n/a |
| container_blob_cold_manifest | n/a | n/a | n/a |
| container_blob_hot | n/a | n/a | n/a |
| container_blob_same_digest_concurrent | n/a | n/a | n/a |
| container_manifest_cold | n/a | n/a | n/a |
| container_manifest_hot | n/a | n/a | n/a |
| container_multi_repo_blob | n/a | n/a | n/a |
| container_multi_repo_blob_manifest | n/a | n/a | n/a |
| container_multi_repo_manifest | n/a | n/a | n/a |
| container_multi_repo_tags | n/a | n/a | n/a |
| container_referrers | n/a | n/a | n/a |
| container_tags | n/a | n/a | n/a |
| health_baseline | n/a | n/a | n/a |
| maven_release_hot | n/a | n/a | n/a |
| mixed_container_blob | n/a | n/a | n/a |
| mixed_container_manifest | n/a | n/a | n/a |
| mixed_maven_release | n/a | n/a | n/a |
| mixed_npm_metadata | n/a | n/a | n/a |
| mixed_npm_tarball | n/a | n/a | n/a |
| mixed_pypi_simple | n/a | n/a | n/a |
| mixed_pypi_wheel | n/a | n/a | n/a |
| npm_metadata_hot | n/a | n/a | n/a |
| npm_tarball_hot | n/a | n/a | n/a |
| pypi_simple_hot | n/a | n/a | n/a |
| pypi_wheel_hot | n/a | n/a | n/a |

## Endpoint Throughput

| name | sqlite req/s | mysql req/s | postgres req/s |
| --- | ---: | ---: | ---: |
| container_blob_cold | 0.00 | 0.00 | 0.00 |
| container_blob_cold_manifest | 0.00 | 0.00 | 0.00 |
| container_blob_hot | 3.39 | 3.86 | 2.93 |
| container_blob_same_digest_concurrent | 3.05 | 3.63 | 3.57 |
| container_manifest_cold | 0.00 | 0.00 | 0.00 |
| container_manifest_hot | 34.21 | 43.47 | 43.18 |
| container_multi_repo_blob | 5.94 | 7.17 | 6.52 |
| container_multi_repo_blob_manifest | 11.87 | 14.33 | 13.04 |
| container_multi_repo_manifest | 5.91 | 7.15 | 6.50 |
| container_multi_repo_tags | 5.88 | 7.12 | 6.46 |
| container_referrers | 286.84 | 301.83 | 292.09 |
| container_tags | 286.84 | 301.84 | 292.10 |
| health_baseline | 181.08 | 179.26 | 183.64 |
| maven_release_hot | 5.85 | 6.91 | 6.97 |
| mixed_container_blob | 1.82 | 2.08 | 1.38 |
| mixed_container_manifest | 1.86 | 2.09 | 1.38 |
| mixed_maven_release | 1.86 | 2.08 | 1.37 |
| mixed_npm_metadata | 1.80 | 2.08 | 1.37 |
| mixed_npm_tarball | 1.80 | 2.08 | 1.37 |
| mixed_pypi_simple | 1.81 | 2.06 | 1.36 |
| mixed_pypi_wheel | 1.85 | 2.08 | 1.37 |
| npm_metadata_hot | 5.52 | 6.25 | 5.63 |
| npm_tarball_hot | 5.90 | 6.84 | 7.10 |
| pypi_simple_hot | 5.77 | 6.36 | 6.90 |
| pypi_wheel_hot | 5.13 | 6.36 | 7.04 |

## Notes

- Compare reports generated with the same `REGIMUX_STRESS_PROFILE`; mixed profiles are shown as `mixed`.
- Cold scenario rows are short shared-iteration baselines; sustained throughput comparisons should focus on hot and mixed scenarios.
- JSON output contains the same store, scenario, and endpoint tables for CI trend storage.
