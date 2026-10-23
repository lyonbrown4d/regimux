# Container memory acceptance harness

This harness layers over `tests/concurrency/compose.yml`. It uses the existing deterministic six-ecosystem fixture with a 1,024-byte synthetic payload and points the PostgreSQL/MySQL S3 clients through a bounded streaming proxy with a 100 ms delay on PUT requests. The proxy copies request and response bodies in 32 KiB blocks, preserves the signed `Host`, path, authorization and body framing, and forwards chunk extensions and trailers without buffering complete parts.

Use a fresh Compose project and database/object volumes. Set `REGIMUX_CONCURRENCY_IMAGE` to the locally built candidate image selected for acceptance. Start one backend at a time:

```powershell
$env:REGIMUX_CONCURRENCY_IMAGE = '<local image tag>'
$project = 'regimux-memory-candidate01-pg'
$env:REGIMUX_CONCURRENCY_NETWORK = $project
docker compose -p $project -f tests/concurrency/compose.yml -f tests/memory/compose.yml --profile postgres up -d regimux-postgres regimux-postgres-peer
./tests/memory/run-distinct-bursts.ps1 -Backend postgres -RunId candidate01 -Project $project
```

Use a separate fresh project name, such as `regimux-memory-candidate01-mysql`, with `--profile mysql` and `-Backend mysql` for MySQL. The Compose overlay caps each RegiMux instance at 512 MiB with `GOMEMLIMIT=384MiB`; it publishes no ports and uses an internal network. Preserve logs and JSON files under `tmp/memory-20261021`.

The script runs all 13 targets individually. Each case starts 512 k6 VUs by default; each VU uses a unique wave suffix and therefore requests a distinct cold object key. VUs alternate between the primary and peer RegiMux replica. Fixture metadata is prepared in batches of 32 to bound fixture-side concurrency. k6 verifies status and the complete SHA-256 or the existing rewrite markers, then requires exactly one upstream GET for each distinct key. The runner samples Docker stats each second and saves per-container final state, restart count, OOM status, logs, and k6 JSON. Both runners reject a case if an app or dependency stopped, was OOM-killed, or restarted. A fresh `RunId` is required for each run; the script refuses to overwrite existing artifacts.

This runner covers only distinct-key cold fills. For the existing hot 512/1024 matrix and resource samples on the same memory-capped stack, set its network and project to the values used above:

```powershell
$env:REGIMUX_CONCURRENCY_NETWORK = $project
./tests/concurrency/run-matrix.ps1 -Backend postgres -RunId candidate01 -Project $project -ReportsPath tmp/memory-20261021/hot-postgres
```

Use `-Backend mysql` and a fresh `RunId`/`ReportsPath` for MySQL. The matrix script remains the source of hot-load and resource sampling evidence.

The Maven mirror regression has a separate profile using the same candidate image:

```powershell
$project = 'regimux-memory-candidate01-maven8'
$env:REGIMUX_CONCURRENCY_NETWORK = $project
docker compose -p $project -f tests/concurrency/compose.yml -f tests/memory/maven-compose.yml --profile maven8 up -d regimux-maven8
docker compose -p $project -f tests/concurrency/compose.yml -f tests/memory/maven-compose.yml --profile maven8 run --rm maven8-client
```

The client checks the complete POM checksum after a 404-to-200 mirror fallback, a cache hit without another upstream request, HEAD content length, all-missing results, preservation of a later 503 response, and a terminal 401 without fallback. Save its output together with the app logs and final container state.

A harness definition or a successful Compose configuration check is not an acceptance result. Run the Go regression suite and race checks in containers, then run the Maven profile, the existing concurrency matrix, and both distinct-key backends against one image built from the same frozen source. Record the image ID, source hashes, client exit status, OOM status, and restart count. Docker stats reports container memory usage; label it separately from process RSS and retain missing samples as missing evidence.

The PyPI transfer fixture (`pypi_transfer_fixture.py`) serves the pinned 75,545,685-byte wheel from `/wheel/public.whl`, verifies its SHA-256, and interrupts its first full transfer after 8 MiB with a 31-second idle period. Run it in a private container network with the candidate service. The acceptance client (`pypi_transfer_acceptance.py`) requires Python 3.12, uv, and pip; mount the candidate's SQLite/local-object data read-only at `/cache` and a fresh writable report directory at `/reports`. `REGIMUX_BASE_URL` and `PYPI_FIXTURE_URL` default to `http://regimux:8080` and `http://fixture:8081`.

The client checks the complete wheel hash after recovery, absence of partial cache publication during the interruption, warm full/range/HEAD responses, `If-Range`, and 416. The shared 10-second body inactivity budget must resume the transfer before the client's 20-second deadline while the source retains its 31-second stall. It also locks and installs the fixture's universal demo wheel with uv, imports it, and downloads both the demo and original wheel with pip. The original macOS wheel is downloaded and hashed without executing it. Retain the fixture's exact continuation offsets, client reports, process memory samples, and container OOM/restart state alongside the chosen image identity.
