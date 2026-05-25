# RegiMux

RegiMux is a read-only OCI / Docker Registry V2 multi-upstream proxy mirror gateway.

This repository currently contains a runnable skeleton based on the design document:

- `regimuxd`: single-process daemon entrypoint.
- Cobra-based command line with a single config-driven daemon mode.
- Strongly typed `httpx.Endpoint` routes for health, Registry V2 ping, manifests, blobs, tags, and referrers.
- Alias-based upstream routing such as `/v2/hub/library/alpine/manifests/latest`.
- Raw `net/http` upstream client with basic bearer-token challenge handling.
- DI and lifecycle wiring with `github.com/arcgolabs/dix`, including endpoint collection injection into the HTTP server.
- Config loading with `github.com/arcgolabs/configx`.
- Logging with `github.com/arcgolabs/logx` on top of `log/slog`.
- Event bus wiring with `github.com/arcgolabs/eventx`.
- `collectionx` usage for ordered upstream registry snapshots.

Run locally:

```bash
go run ./cmd/regimuxd --config configs/regimux.yaml
```

Then try:

```bash
curl -i http://localhost:5000/v2/
curl -i -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
  http://localhost:5000/v2/hub/library/alpine/manifests/latest
```
