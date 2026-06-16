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

auth {
  enabled = false
}

log {
  level = "info"
  console = true
}

cache {
  backend = "redis"
  prefix = "regimux-it"
  default_ttl = "10m"

  redis {
    addrs = ["redis:6379"]
  }

  blob {
    stream_and_cache = true
  }
}

store {
  meta {
    driver = "sqlite"
    path = "/var/lib/regimux/regimux.db"
  }

  object {
    driver = "local"
    path = "/var/lib/regimux/objects"
  }
}

scheduler {
  enabled = false
}

worker {
  io_concurrency = 8
  lease_concurrency = 16
}

container {
  hub {
    registry = "https://registry-1.docker.io"
    mirrors = ["https://docker.m.daocloud.io"]
    mirror_policy = "ordered"
    default_namespace = "library"

    blob {
      mirror_policy = "round_robin"
      max_concurrent_attempts = 2
    }

    probe {
      enabled = false
    }

    auth {
      type = "anonymous"
    }

    http {
      retry {
        enabled = true
        max_retries = 2
        wait_min = "100ms"
        wait_max = "1s"
      }
    }
  }
}

npm {
  default {
    registry = "https://registry.npmmirror.com"
    mirrors = ["https://registry.npmjs.org"]
    mirror_policy = "ordered"
  }
}

pypi {
  default {
    registry = "https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple"
    mirrors = ["https://pypi.org"]
    mirror_policy = "ordered"
  }
}

maven {
  central {
    registry = "https://maven.aliyun.com/repository/public"
    mirrors = ["https://repo.maven.apache.org/maven2"]
    mirror_policy = "ordered"
  }
}
