# Offline high-concurrency acceptance fixture

This fixture serves deterministic package bytes for OCI, Go modules, npm, PyPI, Maven, and Node.js distribution archives. The six tested RegiMux upstream aliases point to the `fixture` service on the isolated Compose network. RegiMux may merge additional default aliases into its effective configuration; these are not exercised, and the network blocks public egress. The network is marked `internal`, the fixture makes no outbound requests, and no service publishes a host port.

The HTTP service is implemented in `fixture.py` and `assets.py` using Python's standard library. Artifacts are generated in memory and include content hashes and body lengths in the administrative manifest. Each archive contains deterministic incompressible payload bytes derived from the wave ID, 131,072 bytes by default. Override `FIXTURE_PAYLOAD_BYTES` from 0 to 1,048,576 to tune artifact size. The fixture delays artifact responses by 100 ms by default so a cold concurrent burst overlaps at the upstream. Override `FIXTURE_DELAY_MS` or request `?delay_ms=0` for a different delay. Delay is clamped to 0–10,000 ms.

## Administration API

Use the service address `http://fixture:8081` from another Compose container.

* `GET /__fixtures?wave=wave-001` returns `{wave, delay_ms, targets:[...]}`. Each target has `name`, `ecosystem`, the full RegiMux `path` including alias, `upstream_path` (the stats counter key), and expected `status`. Targets with unchanged responses have `sha256`; metadata/index targets changed by RegiMux have `markers` describing the rewrite.
* `GET /__stats` returns `{requests:{upstream_path:{GET:n,HEAD:n}}}` with thread-safe counters keyed by the actual upstream URL path and split by method. It counts successful artifact responses only.
* `POST /__reset` clears all upstream counters. `POST /__reset?prefix=/go/` clears only paths beginning with that prefix. It does not change RegiMux's application cache.
* `GET /healthz` is a local health probe.

Wave IDs match `[a-z0-9][a-z0-9-]{0,47}`. Every ecosystem embeds the wave in its package or artifact identity, making separate cold waves address distinct cache keys. Do not reuse a wave when measuring cold fills. OCI manifest references contain wave-specific blob digests; retrieve the manifest first, then request its config and layer blob paths. The `__fixtures` manifest includes those resolved paths.

The fixture uses an in-memory, thread-safe counter and a threaded HTTP server. A process restart clears all stats. Admin endpoints are intended for the private test network and do not accept URLs, hosts, file paths, or commands from callers.

## Compose profiles

The project and isolated network are named `regimux-concurrency-20261016`. The RegiMux image defaults to `ghcr.io/lyonbrown4d/regimux:1.3.5`; override it with `REGIMUX_CONCURRENCY_IMAGE` when testing a locally built image. The client uses a pinned k6 image; override it with `REGIMUX_CONCURRENCY_K6_IMAGE` if needed. Run `docker compose -f tests/concurrency/compose.yml config` to validate interpolation and Compose structure without starting services.

* `sqlite` runs one RegiMux instance with local SQLite metadata and a local object store, plus Redis for leases.
* `postgres` runs two RegiMux instances with PostgreSQL metadata and SeaweedFS S3 objects.
* `mysql` runs two RegiMux instances with MySQL metadata and SeaweedFS S3 objects.
* `clients` enables the pinned k6 client service, which runs `load.ts`; reports are written to the ignored `tmp/concurrency-20261016/reports` directory.

The PostgreSQL and MySQL profiles start SeaweedFS and a bootstrap service reusing the existing acceptance tools image and its locked boto3 dependency that creates the test bucket on the private network before either RegiMux instance starts. To prepare the PostgreSQL matrix, run `docker compose --profile postgres -f tests/concurrency/compose.yml up -d regimux-postgres regimux-postgres-peer`; use `--profile mysql` and the corresponding service names for MySQL. Both profiles provide the load client's fixed service aliases `regimux` and `regimux-peer`. Run the `client` service with `--profile clients`, setting `BASE_URLS=http://regimux:8080,http://regimux-peer:8080` for a two-replica run. The pair shares its isolated named data volume and metadata store. Keep Compose commands scoped to this file and project; `down -v` removes only this fixture's named volumes.

Run `docker compose --profile sqlite -f tests/concurrency/compose.yml up -d regimux-sqlite` for the single-instance SQLite matrix. For a cold burst, set `MODE=burst`, `CONCURRENCY=256` (or 512/1024), and a fresh `WAVE`; for sustained hot traffic, set `MODE=hot` and `DURATION`. `TARGET=all` exercises all six ecosystems. No public registry or object store is contacted by these requests.

Run one database profile at a time so the `regimux` DNS alias resolves to the intended service. Stop the previous profile's application containers before starting another profile. Each application has a 2 CPU / 1 GiB limit; the k6 client has 4 CPUs / 3 GiB so generating and hashing requests has a separate budget.

For example, after starting SQLite, run a single cold-object burst:

```powershell
docker compose -f tests/concurrency/compose.yml --profile clients run --rm -e MODE=burst -e CONCURRENCY=512 -e TARGET=go-zip -e WAVE=unique-go-001 -e REPORT=sqlite-go-001 client
```

Repeat with all 13 target names from `__fixtures` to test 512 callers for each individual object. `TARGET=all` divides the VUs across targets; it does not mean 512 callers for each target. For sustained traffic use `MODE=hot`, `TARGET=all`, `DURATION=30s`, and 512 or 1024 VUs. Setup warms every selected artifact on each replica; teardown requires zero additional upstream GETs. A cold burst requires exactly one upstream GET for each selected artifact, except OCI manifests and streamed PyPI wheels, whose current coalescing is process scoped: their bound is one GET per replica. Both modes verify status and complete response hashes, or metadata rewrite markers, and fail on corruption. `MIXED_METHODS=1` alternates GET/HEAD and checks that HEAD responses have no body.

Run `python /fixture/verify_links.py --wave WAVE` inside the fixture container to follow npm and PyPI metadata links and compare the downloaded bytes with the advertised hashes. Reusing a wave from an older application image checks upgrade behavior with existing metadata cache entries.

Aggregate k6 JSON and optional resource samples with `report.py`, running Python in a container and mounting the reports directory. Pass `--directory`, a case filename `--prefix`, `--markdown`, and `--json` output paths. Reports distinguish configured VUs, completed requests, and observed response rates; missing resource samples remain unavailable. Docker samples are observed maxima, not continuous peak measurements. A report generator exit status only indicates successful aggregation: inspect the recorded thresholds and client exit status to determine acceptance.

For the complete matrix on a running backend, PowerShell 7 users can run:

```powershell
./tests/concurrency/run-matrix.ps1 -Backend sqlite -RunId unique001
```

Use `postgres` or `mysql` after switching the active application profile. The script runs 13 separate cold 512-VU objects, sustained 512/1024-VU mixed traffic, and a 512-VU GET/HEAD manifest burst. It samples container resources and retains client logs, threshold summaries, OOM state, and restart counts. It stops at the first failed case and refuses to overwrite existing reports; choose a fresh run ID for a new cold matrix. `-OnlyHot` runs the two sustained cases and the method-mixing case during debugging. Compile, race tests, and static analysis must finish before starting pressure runs so they do not compete for resources.

## Artifact scope

The assets are synthetic and sufficient for protocol clients to fetch and validate the package forms used by acceptance. The OCI layer is a small gzip tar with a matching config and digest. The Go proxy serves `.info`, `.mod`, and module-rooted `.zip`; npm metadata names a tarball that RegiMux rewrites to its local proxy route; PyPI serves a simple index and a pure-Python wheel with a SHA-256 fragment; Maven serves a POM and JAR; dist serves an XZ-compressed tar archive. The dist payload is a placeholder file, not an executable Node.js build.
