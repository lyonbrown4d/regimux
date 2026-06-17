# RegiMux k6 Load Comparison

- generated_at: 2026-06-17T02:45:00Z
- profile: load
- base_url: http://regimux:8080
- workload: warm real artifacts, then run isolated hot-cache scenarios and one mixed ecosystem scenario
- artifacts: lodash@4.17.21, six==1.17.0, commons-io:commons-io:2.16.1, busybox:1.36.1

## Overall

| metadata store | requests | req/s | failed | avg | p95 | p99 | data received |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| sqlite | 115079 | 564.21 | 0.00% | 22.46 ms | 201.21 ms | 369.56 ms | 3.59 GiB |
| mysql | 116791 | 574.05 | 0.00% | 22.13 ms | 188.17 ms | 331.96 ms | 3.91 GiB |
| postgres | 122313 | 604.06 | 0.00% | 21.14 ms | 167.36 ms | 313.71 ms | 4.23 GiB |

## Scenario p95

| scenario | sqlite | mysql | postgres |
| --- | ---: | ---: | ---: |
| health_baseline | 0.26 ms | 0.26 ms | 0.25 ms |
| npm_metadata_hot | 224.32 ms | 202.33 ms | 185.14 ms |
| npm_tarball_hot | 244.86 ms | 200.60 ms | 187.90 ms |
| pypi_simple_hot | 236.55 ms | 197.87 ms | 191.36 ms |
| pypi_wheel_hot | 236.87 ms | 211.04 ms | 196.10 ms |
| maven_release_hot | 225.72 ms | 205.70 ms | 203.05 ms |
| container_manifest_hot | 3.10 ms | 4.46 ms | 2.84 ms |
| container_blob_range_hot | 270.86 ms | 219.56 ms | 222.51 ms |
| mixed_ecosystems | 521.04 ms | 432.01 ms | 416.95 ms |

## Scenario Throughput

| scenario | sqlite req/s | mysql req/s | postgres req/s |
| --- | ---: | ---: | ---: |
| health_baseline | 373.51 | 375.16 | 379.93 |
| npm_metadata_hot | 12.51 | 12.82 | 14.89 |
| npm_tarball_hot | 11.65 | 13.27 | 14.75 |
| pypi_simple_hot | 12.04 | 13.15 | 14.51 |
| pypi_wheel_hot | 11.93 | 12.87 | 14.15 |
| maven_release_hot | 12.55 | 13.56 | 14.02 |
| container_manifest_hot | 96.47 | 95.62 | 110.87 |
| container_blob_range_hot | 6.75 | 7.55 | 8.11 |
| mixed_ecosystems | 26.75 | 30.00 | 32.78 |

## Notes

- All three runs completed with 0.00% request failures.
- The hot-cache path is mostly Redis/object-store/body streaming work; the metadata backend still shows up in mixed contention and artifact metadata paths.
- On this local run, Postgres produced the best overall p95 and mixed-ecosystem p95, followed by MySQL, then sqlite.
- These numbers are local-environment measurements. Use the generated JSON reports for repeatable CI comparison or trend storage.
