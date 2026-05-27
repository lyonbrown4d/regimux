server {
  listen = ":5000"
}

upstreams {
  hub {
    registry = "https://registry-1.docker.io"
  }
}
