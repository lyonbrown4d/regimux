# RegiMux 文档

RegiMux 是面向研发和 CI 环境的只读 Dependency Proxy（依赖代理）。Docker/containerd、Go、npm、PyPI 或 Maven 客户端把 RegiMux 配成依赖入口；RegiMux 将 miss 转发到配置的上游，缓存不可变制品，并维护 metadata，用于可观测性、清理和后台刷新。

container registry、Go modules、npm、PyPI 和 Maven 都是一等依赖生态，分别拥有独立配置块、endpoint service 和 runtime capability。调度器消费各生态 runtime 声明的 job 和 capability，而不是直接导入具体生态的编排逻辑。

## 入门

- [使用指南](usage.md)：通过发布产物运行 RegiMux、把客户端配置为使用 RegiMux 作为依赖代理、使用 Admin UI 和 Docker Compose 示例。
- [配置](configuration.md)：配置文件、命令行覆盖、环境变量和 dotenv。
- [存储](storage.md)：元数据驱动和对象存储驱动。
- [调度器](scheduler.md)：清理、容量控制、mirror 探测和预测预拉取。
- [认证](auth.md)：Docker login、配置用户和仓库权限范围。
- [Admin UI](admin.md)：内嵌管理页面和手动刷新。

## 参考

- [设计](design.md)：架构和协议设计。
- [Roadmap](roadmap.md)：计划工作和非目标。
- [发布](releases.md)：CI、GoReleaser、包产物和 Docker 镜像。
- [Compose](compose.md)：可运行的 Docker Compose 示例。

英文文档：[../en/README.md](../en/README.md)
