# Admin UI

The Admin UI is embedded into the RegiMux binary. It uses Fiber template rendering, embedded templates, embedded i18n resources, Tailwind CSS from CDN, and htmx from CDN.

Open:

```text
http://localhost:5000/admin
```

## Security

When `auth.enabled = true`, Admin UI is protected with HTTP Basic using the same configured users as Registry auth.

## Language and Theme

Admin UI supports English and Chinese. Locale resources are embedded in the binary.

The UI follows the browser or operating system light/dark preference automatically.

## Views

Current views:

- dashboard
- upstream health
- pull and activity history
- cache status
- storage and large blobs
- scheduler jobs, prefetch runs, and prefetch outcomes
- manual sync
- auth audit
- effective configuration

## Manual Sync

Manual sync is ecosystem-aware. In the upstream selector you can choose

- `container:<alias>` for OCI/container images
- `go:<alias>` for Go module proxy
- `npm:<alias>` for npm
- `pypi:<alias>` for PyPI
- `maven:<alias>` for Maven

For each ecosystem, fill the same `Repository` / `Reference` fields:

- `container`: repository format uses container image path, e.g. `library/node` with reference `20`.
- `go`: repository is the module path, e.g. `github.com/pkg/errors`, reference is version/tag like `v0.9.1`.
- `npm`: repository is the package name, reference is the version/tag, e.g. `react`, `18.2.0`.
- `pypi`: repository is package name, reference is version/tag.
- `maven`: repository is group/artifact path, reference is version/version segment.

Examples:

```text
container:hub / repository=library/node / reference=20
go:default / repository=github.com/pkg/errors / reference=v0.9.1
npm:default / repository=react / reference=18.2.0
pypi:default / repository=urllib3 / reference=2.2.0
maven:central / repository=com/fasterxml/jackson/core/jackson-databind / reference=2.16.1
```

The job is created as async and can be viewed from the sync result panel. It prewarms the ecosystem cache path and records outcomes in metadata.
