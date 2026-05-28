# Releases

RegiMux releases are driven by GitHub Actions and GoReleaser. Published artifacts are available from [GitHub Releases](https://github.com/lyonbrown4d/regimux/releases).

## Release Trigger

Pushing a tag such as `v0.0.2` runs:

- `go test ./...`
- `golangci-lint run ./...`
- `goreleaser check`
- GoReleaser publish

## GitHub Release Artifacts

GitHub Releases include:

- Linux archives: `tar.gz`
- macOS archives: `tar.gz`
- Windows archives: `zip`
- standalone Windows binary artifact
- Linux packages: `deb` and `rpm`
- checksums

Linux binaries are compressed with UPX during the GoReleaser pipeline.

## Docker Images

Images are published to GitHub Container Registry:

```text
ghcr.io/lyonbrown4d/regimux:latest
ghcr.io/lyonbrown4d/regimux:alpine
ghcr.io/lyonbrown4d/regimux:v0.0.2
ghcr.io/lyonbrown4d/regimux:v0.0.2-alpine
ghcr.io/lyonbrown4d/regimux:latest-debian
ghcr.io/lyonbrown4d/regimux:v0.0.2-debian
```

`latest` is the Alpine image. Use the `-debian` suffix for Debian-based images.

## Package Layout

deb/rpm packages install:

- `/usr/bin/regimuxd`
- `/etc/regimux/regimux.hcl`
- `/lib/systemd/system/regimuxd.service`
- `/var/lib/regimux`
