server {
  listen = ":8080"
  public_url = "http://regimux:8080"
  middleware {
    healthcheck {
      enabled = true
      liveness_path = "/livez"
      readiness_path = "/readyz"
    }
  }
}
auth { enabled = false }
log {
  level = "info"
  console = true
}
cache {
  backend = "redis"
  prefix = "regimux-container-acceptance"
  redis { addrs = ["redis:6379"] }
  blob { stream_and_cache = true }
}
store {
  meta {
    driver = "postgres"
    dsn = "postgresql://regimux:acceptance-only@postgres:5432/regimux?sslmode=disable"
  }
  object {
    driver = "s3"
    s3 {
      bucket = "regimux-acceptance"
      prefix = "cache"
      region = "us-east-1"
      endpoint = "http://s3:9000"
      access_key_id = "acceptance-only"
      secret_access_key = "acceptance-only-secret"
      force_path_style = true
    }
  }
}
scheduler {
  enabled = true
  distributed_lock = true
  lock_ttl = "30m"
  cleanup {
    enabled = true
    cron = "0 3 * * *"
    timezone = "Asia/Shanghai"
    unused_for = "168h"
    max_deletes = 1000
    distributed = true
  }
  prefetch { enabled = false }
  manifest_refresh { enabled = false }
}
worker {
  io_concurrency = 8
  lease_concurrency = 16
}
default_container_alias = "hub"
container {
  hub {
    registry = "https://registry-1.docker.io"
    mirrors = ["http://restricted-mirror:5000", "https://docker.m.daocloud.io"]
    mirror_policy = "ordered"
    mirror_forbidden_policy = "public"
    default_namespace = "library"
    probe { enabled = false }
    auth { type = "anonymous" }
  }
}
go {
  default { registry = "https://proxy.golang.org" }
}
npm {
  default { registry = "https://registry.npmjs.org" }
}
pypi {
  default {
    registry = "https://pypi.org"
    mirrors = ["https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple"]
    http { attempt_timeout = "10s" }
  }
}
maven {
  central {
    registry = "http://restricted-mirror:5000/maven"
    mirrors = ["https://repo.maven.apache.org/maven2", "https://maven.aliyun.com/repository/public"]
    http { attempt_timeout = "10s" }
  }
}
dist {
  nodejs {
    registry = "https://nodejs.org/dist"
    allow = ["v*/SHASUMS256.txt", "v*/node-*-linux-x64.tar.xz"]
  }
}
