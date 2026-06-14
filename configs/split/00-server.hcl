server {
  listen = ":8080"
  public_url = "http://localhost:8080"
  read_timeout = "30s"
  write_timeout = "0s"
  idle_timeout = "120s"

  middleware {
    request_id {
      enabled = true
      header = "X-Request-ID"
    }

    healthcheck {
      enabled = true
      liveness_path = "/livez"
      readiness_path = "/readyz"
    }

    etag {
      enabled = true
    }

    compress {
      enabled = true
      level = "default"
    }
  }
}

auth {
  enabled = false
  service = "regimux"
  issuer = "regimux"
  realm = ""
  token_secret = ""
  token_ttl = "15m"
}

log {
  level = "info"
  console = true
  add_caller = false
}

cache {
  backend = ""
  prefix = "regimux"
  default_ttl = "10m"

  manifest {
    tag_ttl = "10m"
    stale_if_error = true
    max_stale = "168h"
  }

  blob {
    stream_and_cache = true
  }
}
