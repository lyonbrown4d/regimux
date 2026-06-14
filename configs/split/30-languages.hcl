go {
  default {
    registry = "https://proxy.golang.org"
  }
}

npm {
  default {
    registry = "https://registry.npmjs.org"
    tag_ttl = "5m"
  }
}

pypi {
  default {
    registry = "https://pypi.org"
  }
}

maven {
  central {
    registry = "https://repo.maven.apache.org/maven2"
    tag_ttl = "5m"
  }
}
