server {
  listen = ":8080"
  public_url = "http://localhost:8080"
}

log {
  level = "info"
  console = true
}

cache {
  backend = "memory"
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
  distributed_lock = false

  cleanup {
    enabled = true
    interval = "1h"
    unused_for = "168h"
    max_scan = 0
    max_deletes = 1000
    max_bytes = 0
    target_bytes = 0
  }

  prefetch {
    enabled = false
  }
}

upstreams {
  hub {
    type = "oci"
    registry = "https://registry-1.docker.io"
    default_namespace = "library"

    blob {
      mirror_policy = "ordered"
    }

    auth {
      type = "anonymous"
    }
  }

  golang {
    type = "go"
    registry = "https://proxy.golang.org"
  }
}
