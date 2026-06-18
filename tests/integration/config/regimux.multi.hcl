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
  prefix = "regimux-it-multi"
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
    registry = "http://fake-registry:5000"
    mirror_policy = "ordered"
    default_namespace = "library"

    blob {
      mirror_policy = "ordered"
      max_concurrent_attempts = 1
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
        max_retries = 1
        wait_min = "50ms"
        wait_max = "250ms"
      }
    }
  }
}
