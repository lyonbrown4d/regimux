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

    request_logger {
      enabled = false
    }

    healthcheck {
      enabled = true
      liveness_path = "/livez"
      readiness_path = "/readyz"
    }

    etag {
      enabled = true
    }

    security_headers {
      enabled = true
      content_security_policy = ""
      cross_origin_embedder_policy = "unsafe-none"
      hsts_max_age = 0
    }

    compress {
      enabled = true
      level = "default"
    }

    rate_limit {
      enabled = false
      max = 60
      expiration = "1m"
    }

    csrf {
      enabled = false
      idle_timeout = "30m"
      cookie_name = "regimux_csrf"
      cookie_secure = false
      trusted_origins = []
    }

    pprof {
      enabled = false
      prefix = ""
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

  # Example:
  #
  # users {
  #   alice {
  #     password_hash = "$2a$12$replace-with-bcrypt-hash"
  #     repositories = ["hub/*", "ghcr/my-org/*"]
  #     groups = ["developers"]
  #   }
  # }
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
    stream_and_cache = true

    small_cache {
      enabled = false
      max_size_bytes = 4194304
      ttl = "24h"
    }
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
    driver = "sqlite"
    dsn = ""
    path = "data/regimux.db"
    # MySQL example:
    # driver = "mysql"
    # dsn = "regimux:secret@tcp(mysql:3306)/regimux?parseTime=true"
    # PostgreSQL example:
    # driver = "postgres"
    # dsn = "postgres://regimux:secret@postgres:5432/regimux?sslmode=disable"
  }

  object {
    driver = "local"
    path = "data/objects"

    # S3-compatible object storage example:
    #
    # driver = "s3"
    # path = ""
    #
    # s3 {
    #   bucket = "regimux-objects"
    #   prefix = "cache"
    #   region = "us-east-1"
    #   endpoint = "http://minio:9000"
    #   access_key_id = "regimux"
    #   secret_access_key = "change-me"
    #   session_token = ""
    #   profile = ""
    #   force_path_style = true
    #   part_size = 16777216
    #   upload_concurrency = 4
    # }
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
    dry_run = false
    distributed = false
  }

  prefetch {
    enabled = false
    interval = "30m"
    max_records = 200
    min_pull_count = 2
    tags_page_size = 1000
    max_candidates_per_repo = 3
    max_version_distance = 5
    max_bytes = 0
    max_tasks = 0
    max_repositories = 0
    failure_backoff = "1h"
    retry_window = "24h"
    distributed = true
  }

  refresh {
    enabled = true
    window = "10m"
    distributed = true
  }

  manifest_refresh {
    enabled = false
    interval = "30m"
    distributed = true

    ecosystems {
      container {
        enabled = false
        interval = "15m"
      }

      go {
        enabled = false
      }

      npm {
        enabled = false
      }

      pypi {
        enabled = false
      }

      maven {
        enabled = false
      }
    }
  }
}

worker {
  # Default is runtime CPU count * 2 + 1; tune this for network IO.
  # io_concurrency = 17
  lease_concurrency = 64
}

docker {
  enabled = false
  host = ""
  observe = true

  prewarm {
    enabled = false
    registry = ""
    alias = "hub"
    images = []
    timeout = "10m"
    platform = ""
  }
}

default_container_alias = "hub"

container {
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

    blob {
      mirror_policy = "latency"
      top_n = 3
      max_concurrency_per_endpoint = 0
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

    http {
      timeout = "0s"

      http2 {
        enabled = false
      }

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

      http2 {
        enabled = false
      }

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

      http2 {
        enabled = false
      }

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

      http2 {
        enabled = false
      }

      retry {
        enabled = true
        max_retries = 2
        wait_min = "100ms"
        wait_max = "1s"
      }
    }
  }
}

go {
  default {
    registry = "https://proxy.golang.org"
    mirrors = []

    probe {
      enabled = false
      interval = "1m"
      timeout = "3s"
      cooldown = "2m"
      jitter = "10s"
    }

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

npm {
  default {
    registry = "https://registry.npmjs.org"
    tag_ttl = "5m"

    probe {
      enabled = false
      interval = "1m"
      timeout = "3s"
      cooldown = "2m"
      jitter = "10s"
    }

    auth {
      type = "anonymous"
    }
  }
}

pypi {
  default {
    registry = "https://pypi.org"

    probe {
      enabled = false
      interval = "1m"
      timeout = "3s"
      cooldown = "2m"
      jitter = "10s"
    }

    auth {
      type = "anonymous"
    }
  }
}

maven {
  central {
    registry = "https://repo.maven.apache.org/maven2"
    tag_ttl = "5m"

    probe {
      enabled = false
      interval = "1m"
      timeout = "3s"
      cooldown = "2m"
      jitter = "10s"
    }

    auth {
      type = "anonymous"
    }
  }
}

# Maven groups aggregate independent repositories under one logical alias.
# Requests use /maven/{name}/...; group and upstream aliases share one namespace and must be unique.
# Members are searched in order; mirrors remain same-content failover endpoints.
#
# maven_groups {
#   public {
#     members = ["central", "gradle_plugins", "spring"]
#     fallback_on_error = false
#     metadata_policy = "merge"
#   }
# }
#
dist {
  gradle {
    registry = "https://services.gradle.org/distributions"
    mirrors = []
    mirror_policy = "ordered"
    tag_ttl = "24h"
    allow = [
      "gradle-*-bin.zip",
      "gradle-*-all.zip",
    ]

    probe {
      enabled = false
      interval = "1m"
      timeout = "3s"
      cooldown = "2m"
      jitter = "10s"
    }

    auth {
      type = "anonymous"
    }
  }

  # Example: npm-installed Electron can point electron_mirror or ELECTRON_MIRROR
  # at http://<regimux>/dist/electron/ without any npm proxy rewrite.
  #
  # electron {
  #   registry = "https://github.com/electron/electron/releases/download"
  #   mirrors = []
  #   mirror_policy = "ordered"
  #   tag_ttl = "24h"
  #   allow = [
  #     "v*/electron-v*",
  #     "v*/SHASUMS256.txt",
  #     "v*/SHASUMS256.txt.sig",
  #   ]
  #
  #   probe {
  #     enabled = false
  #     interval = "1m"
  #     timeout = "3s"
  #     cooldown = "2m"
  #     jitter = "10s"
  #   }
  #
  #   auth {
  #     type = "anonymous"
  #   }
  # }
  #
  # playwright {
  #   registry = "https://cdn.playwright.dev"
  #   mirrors = []
  #   mirror_policy = "ordered"
  #   tag_ttl = "24h"
  #   allow = ["builds/*", "dbazure/download/playwright/*"]
  # }
  #
  # cypress {
  #   registry = "https://download.cypress.io"
  #   mirrors = []
  #   mirror_policy = "ordered"
  #   tag_ttl = "24h"
  #   allow = ["desktop", "desktop.json", "desktop/*"]
  # }
  #
  # nodejs {
  #   registry = "https://nodejs.org/download/release"
  #   mirrors = []
  #   mirror_policy = "ordered"
  #   tag_ttl = "24h"
  #   allow = ["v*/node-v*", "index.json", "index.tab"]
  # }
  #
  # hashicorp {
  #   registry = "https://releases.hashicorp.com"
  #   mirrors = []
  #   mirror_policy = "ordered"
  #   tag_ttl = "24h"
  #   allow = [
  #     "terraform/*",
  #     "vault/*",
  #     "consul/*",
  #     "nomad/*",
  #   ]
  # }
}
