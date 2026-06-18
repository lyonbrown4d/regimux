container {
  hub {
    registry = "https://registry-1.docker.io"
    mirrors = [
      "https://docker.1ms.run",
      "https://dockerproxy.net",
    ]
    mirror_policy = "ordered"
    default_namespace = "library"
    tag_ttl = "10m"

    blob {
      mirror_policy = "latency"
      top_n = 3
      max_concurrent_attempts = 1
    }
  }

  ghcr {
    registry = "https://ghcr.io"
    tag_ttl = "5m"

  }
}
