server {
  listen = ":8080"
  public_url = "http://localhost:8080"
}

log {
  level = "info"
  console = true
}

cache {
  backend = "valkey"
  prefix = "regimux"

  valkey {
    addrs = ["valkey:6379"]
    db = 0
  }
}

store {
  meta {
    driver = "sqlite"
    dsn = ""
    path = "data/regimux.db"
  }

  object {
    driver = "local"
    path = "data/objects"
  }
}

scheduler {
  enabled = true
  distributed_lock = true
  lock_ttl = "5m"

  cleanup {
    enabled = true
    interval = "1h"
    unused_for = "168h"
    max_scan = 0
    max_deletes = 1000
    max_bytes = 0
    target_bytes = 0
    distributed = true
  }

  prefetch {
    enabled = true
    interval = "30m"
    max_records = 200
    min_pull_count = 2
    tags_page_size = 1000
    max_candidates_per_repo = 3
    max_version_distance = 5
    distributed = true
  }
}

worker {
  probe_concurrency = 16
  prefetch_concurrency = 8
}

upstreams {
  hub {
    type = "oci"
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

    blob {
      mirror_policy = "latency"
      top_n = 3
      max_concurrent_attempts = 1
    }

    probe {
      enabled = true
      interval = "30s"
      timeout = "3s"
      cooldown = "2m"
      jitter = "5s"
    }

    auth {
      type = "anonymous"
    }
  }

  ghcr {
    type = "oci"
    registry = "https://ghcr.io"

    auth {
      type = "anonymous"
    }
  }

  quay {
    type = "oci"
    registry = "https://quay.io"

    auth {
      type = "anonymous"
    }
  }

  golang {
    type = "go"
    registry = "https://proxy.golang.org"

    auth {
      type = "anonymous"
    }
  }
}
