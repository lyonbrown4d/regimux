# Local release workflow

Use the cross-platform Goyek pipeline when GitHub Actions quota is exhausted or a release must be published from a workstation.

~~~sh
task release:local -- --version v1.2.12
~~~

The top-level release runs these stages strictly in order:

~~~text
validate -> release-preflight -> release-artifacts -> release-images -> release-verify
~~~

A failed stage stops later publication. Each publishing stage can be retried independently:

~~~sh
go run ./build release-artifacts --version v1.2.12
go run ./build release-images --version v1.2.12
go run ./build release-verify --version v1.2.12
~~~

Prerequisites:

- The worktree has no tracked changes.
- The target annotated or lightweight tag points at HEAD locally and on origin.
- Docker Desktop or Docker Engine is running with Buildx enabled.
- Docker is logged in to GHCR and Docker Hub.
- GITHUB_TOKEN is set, or gh auth token returns a token.

Preflight validates the application and build modules, checks Docker and Buildx, verifies local and remote tag identity, resolves GitHub credentials, and starts the pinned GoReleaser container once to validate its Go toolchain.

GoReleaser runs in goreleaser/goreleaser:v2.17.0 with GOTOOLCHAIN=auto. This allows the container to honor the Go patch version declared by go.work even when the image was built with an earlier patch release.

Secret command environments are logged as [REDACTED]. The GitHub token is passed to Docker through the process environment and is never embedded in the command line.

The GitHub artifact stage always passes --skip=docker. Multi-platform images are published separately through host Docker Buildx so the release can reuse the host credential helpers on Windows, macOS, and Linux.

Final verification requires all expected archives, packages, executables, and checksums in the GitHub Release and verifies linux/amd64 plus linux/arm64 manifests for the versioned Alpine and Debian images in both GHCR and Docker Hub.
