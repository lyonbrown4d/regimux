server {
  listen = ":5000"
}

upstreams {
  hub {
    type = "oci"
    registry = "https://registry-1.docker.io"
  }

  golang {
    type = "go"
    registry = "https://proxy.golang.org"
  }
}
