# Authentication

RegiMux can run without auth for private networks, or enforce Docker Registry authentication for pulls and Admin UI.

## Enable Auth

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

`password_hash` is preferred for production when a bcrypt hash is available:

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
docker login localhost:5000
docker pull localhost:5000/{containerAlias}/library/alpine:latest
```

The Registry token flow uses the configured service, issuer, secret, and user repository scopes.

## Repository Scopes

Repository patterns support exact matches and prefix wildcards:

```text
{containerAlias}/library/alpine
{containerAlias}/*
{containerAlias}/my-org/*
```

Admin UI reuses the configured users and is protected with HTTP Basic when auth is enabled.

## Upstream Auth

Upstream registries can also be configured with anonymous, basic, bearer, or Docker Hub auth:

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
