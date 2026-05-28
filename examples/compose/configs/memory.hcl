server {
  listen = ":5000"
  public_url = "http://localhost:5000"
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
    max_deletes = 1000
  }

  prefetch {
    enabled = false
  }
}

upstreams {
  hub {
    registry = "https://registry-1.docker.io"
    default_namespace = "library"

    blob {
      mirror_policy = "ordered"
    }

    auth {
      type = "anonymous"
    }
  }
}
