# RegiMux

RegiMux is a read-only dependency proxy for development and CI environments. Package managers and container runtimes point at RegiMux instead of talking to every public upstream directly; RegiMux routes requests to the configured upstreams, caches immutable artifacts in object storage, tracks metadata, and runs background probe, prefetch, refresh, and cleanup work.

Container registry, Go modules, npm, PyPI, Maven, and dist binary distribution mirrors are first-class dependency ecosystems. Each ecosystem keeps its own protocol adapter, endpoint service, runtime capabilities, and configuration namespace, while shared storage, scheduling, auth, and Admin UI code stay common.

## Documentation

- English documentation: [GitHub Wiki](https://github.com/lyonbrown4d/regimux/wiki/en-README)
- 中文文档：[GitHub Wiki](https://github.com/lyonbrown4d/regimux/wiki/zh-README)
