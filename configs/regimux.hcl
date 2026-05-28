server {
  listen = ":5000"
  public_url = "http://localhost:5000"
  read_timeout = "30s"
  write_timeout = "0s"
  idle_timeout = "120s"
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
    driver = "sqlite"
    dsn = ""
    path = "data/regimux.db"
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
    # }

    # SFTP object storage example:
    #
    # driver = "sftp"
    # path = "/srv/regimux/objects"
    #
    # sftp {
    #   addr = "sftp.example.com:22"
    #   username = "regimux"
    #   password = "change-me"
    #   known_hosts_path = "/etc/regimux/known_hosts"
    #   host_key = ""
    #   timeout = "10s"
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
    distributed = true
  }
}

worker {
  probe_concurrency = 16
  prefetch_concurrency = 8
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
