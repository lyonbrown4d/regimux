server {
  listen = ":8080"
  public_url = "http://regimux:8080"
  middleware {
    healthcheck { enabled = true }
    pprof { enabled = true }
  }
}
auth { enabled = false }
log {
  level = "info"
  console = true
}
cache {
  backend = "redis"
  prefix = "regimux-memory-mysql"
  redis { addrs = ["redis:6379"] }
  blob { stream_and_cache = true }
}
store {
  meta {
    driver = "mysql"
    dsn = "regimux:concurrency-only@tcp(mysql:3306)/regimux?parseTime=true"
  }
  object {
    driver = "s3"
    s3 {
      bucket = "regimux-concurrency"
      prefix = "cache"
      region = "us-east-1"
      endpoint = "http://s3-proxy:9000"
      access_key_id = "concurrency-only"
      secret_access_key = "concurrency-only-secret"
      force_path_style = true
    }
  }
}
scheduler { enabled = false }
default_container_alias = "hub"
container {
  hub {
    registry = "http://fixture:8081/oci"
    default_namespace = "library"
    probe { enabled = false }
    auth { type = "anonymous" }
  }
}
go { default { registry = "http://fixture:8081/go" } }
npm { default { registry = "http://fixture:8081/npm" } }
pypi { default { registry = "http://fixture:8081/pypi" } }
maven { central { registry = "http://fixture:8081/maven" } }
dist {
  nodejs {
    registry = "http://fixture:8081/dist"
    allow = ["v*/node-*-linux-x64.tar.xz"]
  }
}
