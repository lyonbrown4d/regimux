# RegiMux Documentation

RegiMux is a read-only developer dependency cache gateway. Container registry, Go, npm, PyPI, and Maven are first-class ecosystems with independent configuration blocks, endpoint services, and runtime capabilities. The scheduler consumes runtime-declared jobs and capabilities instead of importing per-ecosystem orchestration logic.

## Start Here

- [Usage](usage.md): run released artifacts, pull images, use Admin UI, and run Docker Compose examples.
- [Configuration](configuration.md): config files, command-line overrides, environment variables, and dotenv.
- [Storage](storage.md): metadata drivers and object storage drivers.
- [Scheduler](scheduler.md): cleanup, capacity control, mirror probing, and predictive prefetch.
- [Authentication](auth.md): Docker login flow, configured users, and repository scopes.
- [Admin UI](admin.md): embedded admin pages and manual sync.

## Reference

- [Design](design.md): architecture and protocol overview.
- [Roadmap](roadmap.md): planned work and non-goals.
- [Releases](releases.md): CI, GoReleaser, packages, and Docker images.
- [Compose](compose.md): runnable Docker Compose examples.

Chinese documentation: [../zh/README.md](../zh/README.md)
