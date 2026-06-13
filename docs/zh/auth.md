# 认证

RegiMux 可以在内网无认证运行，也可以开启 Docker Registry 认证来保护拉取接口和 Admin UI。

## 开启认证

```hcl
auth {
  enabled = true
  service = "regimux"
  issuer = "regimux"
  token_secret = "replace-with-a-long-random-secret"
  token_ttl = "15m"

  users {
    alice {
      password = "secret"
      repositories = ["hub/*", "ghcr/my-org/*"]
      groups = ["developers"]
    }
  }
}
```

生产环境建议使用 `password_hash` 保存 bcrypt hash：

```hcl
users {
  alice {
    password_hash = "$2a$12$replace-with-bcrypt-hash"
    repositories = ["hub/*"]
  }
}
```

## Docker Login

```bash
docker login localhost:8080
docker pull localhost:8080/{containerAlias}/library/alpine:latest
```

Registry token 流程会使用配置中的 service、issuer、secret 和用户仓库权限范围。

## 仓库权限范围

仓库 pattern 支持精确匹配和前缀通配：

```text
{containerAlias}/library/alpine
{containerAlias}/*
{containerAlias}/my-org/*
```

Admin UI 会复用同一批配置用户；启用认证后，Admin UI 使用 HTTP Basic 保护。

## 上游认证

上游 registry 可以配置 anonymous、basic、bearer 或 Docker Hub 认证：

```hcl
container {
  hub {
    registry = "https://registry-1.docker.io"

    auth {
      type = "dockerhub"
      username = "dockerhub-user"
      password = "dockerhub-token"
    }
  }
}
```
