server {
  listen = ":5000"
  public_url = "http://localhost:5000"
  read_timeout = "30s"
  write_timeout = "0s"
  idle_timeout = "120s"
}

log {
  level = "info"
  console = true
  add_caller = false
}

cache {
  backend = "memory"
  prefix = "regimux"
  default_ttl = "10m"

  memory {
    max_items = 10000
  }

  redis {
    addrs = ["127.0.0.1:6379"]
    username = ""
    password = ""
    db = 0
  }

  valkey {
    addrs = ["127.0.0.1:6379"]
    username = ""
    password = ""
    db = 0
  }

  manifest {
    tag_ttl = "10m"
    stale_if_error = true
    max_stale = "168h"
  }

  blob {
    stream_and_cache = false
  }

  tags {
    ttl = "5m"
    max_page_size = 1000
  }

  referrers {
    ttl = "5m"
    fallback_tag = true
  }
}

store {
  meta {
    driver = "bboltx"
    path = "data/regimux.db"
  }

  object {
    driver = "local"
    path = "data/objects"
  }
}

upstreams {
  hub {
    registry = "https://registry-1.docker.io"
    mirrors = [
      "https://mirror.example.com",
      "https://docker.1ms.run",
      "https://dockerproxy.net",
      "https://proxy.vvvv.ee",
      "https://dockerproxy.link",
    ]
    mirror_policy = "ordered"
    default_namespace = "library"
    tag_ttl = "10m"

    auth {
      type = "anonymous"
    }

    http {
      timeout = "0s"

      retry {
        enabled = true
        max_retries = 2
        wait_min = "100ms"
        wait_max = "1s"
      }
    }
  }

  ghcr {
    registry = "https://ghcr.io"
    tag_ttl = "5m"

    auth {
      type = "anonymous"
    }

    http {
      timeout = "0s"

      retry {
        enabled = true
        max_retries = 2
        wait_min = "100ms"
        wait_max = "1s"
      }
    }
  }

  quay {
    registry = "https://quay.io"
    tag_ttl = "5m"

    auth {
      type = "anonymous"
    }

    http {
      timeout = "0s"

      retry {
        enabled = true
        max_retries = 2
        wait_min = "100ms"
        wait_max = "1s"
      }
    }
  }

  k8s {
    registry = "https://registry.k8s.io"
    tag_ttl = "30m"

    auth {
      type = "anonymous"
    }

    http {
      timeout = "0s"

      retry {
        enabled = true
        max_retries = 2
        wait_min = "100ms"
        wait_max = "1s"
      }
    }
  }
}
