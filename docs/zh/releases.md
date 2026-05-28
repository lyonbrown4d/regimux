# 发布

RegiMux 通过 GitHub Actions 和 GoReleaser 发布。发布产物在 [GitHub Releases](https://github.com/lyonbrown4d/regimux/releases) 中提供。

## 发布触发

推送类似 `v0.0.2` 的 tag 后会执行：

- `go test ./...`
- `golangci-lint run ./...`
- `goreleaser check`
- GoReleaser publish

## GitHub Release 产物

GitHub Releases 包含：

- Linux 压缩包：`tar.gz`
- macOS 压缩包：`tar.gz`
- Windows 压缩包：`zip`
- 独立 Windows 二进制产物
- Linux 包：`deb` 和 `rpm`
- checksums

Linux 二进制会在 GoReleaser 流程中使用 UPX 压缩。

## Docker 镜像

镜像发布到 GitHub Container Registry：

```text
ghcr.io/lyonbrown4d/regimux:latest
ghcr.io/lyonbrown4d/regimux:alpine
ghcr.io/lyonbrown4d/regimux:v0.0.2
ghcr.io/lyonbrown4d/regimux:v0.0.2-alpine
ghcr.io/lyonbrown4d/regimux:latest-debian
ghcr.io/lyonbrown4d/regimux:v0.0.2-debian
```

`latest` 是 Alpine 镜像。需要 Debian 基础镜像时使用 `-debian` 后缀。

## 包布局

deb/rpm 包会安装：

- `/usr/bin/regimuxd`
- `/etc/regimux/regimux.hcl`
- `/lib/systemd/system/regimuxd.service`
- `/var/lib/regimux`
