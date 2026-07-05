# Local release workflow

Use the cross-platform Go helper when GitHub Actions quota is exhausted or a release needs to be published from a workstation.

```sh
go run -tags release ./cmd/regimux-release --version v1.2.7
```

The helper intentionally avoids PowerShell-only behavior. It runs GoReleaser in the official container for GitHub release assets and then uses the host Docker CLI for multi-arch image publishing, so it can reuse the host Docker login state on Windows, macOS, or Linux.

Prerequisites:

- `git` can see the target release tag at `HEAD`.
- `docker` has Buildx enabled and is logged in to GHCR and Docker Hub.
- `GITHUB_TOKEN` is set, or `gh auth token` can return a token.

Useful options:

```sh
go run -tags release ./cmd/regimux-release --dry-run --version v1.2.7
go run -tags release ./cmd/regimux-release --skip-docker --version v1.2.7
go run -tags release ./cmd/regimux-release --skip-github --version v1.2.7
go run -tags release ./cmd/regimux-release --registry-image ghcr.io/owner/regimux --dockerhub-image owner/regimux --version v1.2.7
```

The GitHub release step always passes `--skip=docker` to GoReleaser. Docker images are built separately by the host Docker CLI to avoid Linux GoReleaser containers reading platform-specific Docker credential helpers from the host.