# RegiMux Documentation

RegiMux is a read-only dependency proxy for development and CI environments. Clients configure Docker/containerd, Go, npm, PyPI, Maven, or generic binary distribution downloads to use RegiMux as their dependency endpoint; RegiMux forwards misses to configured upstreams, caches immutable artifacts, and keeps metadata for observability, cleanup, and background refresh.

Container registry, Go modules, npm, PyPI, Maven, and dist are first-class dependency ecosystems with independent configuration blocks, endpoint services, and runtime capabilities. The scheduler consumes runtime-declared jobs and capabilities instead of importing per-ecosystem orchestration logic.

## Start Here

- [Usage](usage.md): run released artifacts, configure clients to use RegiMux as a dependency proxy, use Admin UI, and run Docker Compose examples.
- [Configuration](configuration.md): config files, command-line overrides, environment variables, and dotenv.
- [Storage](storage.md): metadata drivers and object storage drivers.
- [Scheduler](scheduler.md): cleanup, capacity control, mirror probing, and predictive prefetch.
- [Authentication](auth.md): Docker login flow, configured users, and repository scopes.
- [Admin UI](admin.md): embedded admin pages and manual refresh.

## Reference

- [Design](design.md): architecture and protocol overview.
- [Roadmap](roadmap.md): planned work and non-goals.
- [Releases](releases.md): CI, GoReleaser, packages, and Docker images.
- [Compose](compose.md): runnable Docker Compose examples.

Chinese documentation: [../zh/README.md](../zh/README.md)
