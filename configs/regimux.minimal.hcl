server {
  listen = ":8080"
}

container {
  hub {
    registry = "https://registry-1.docker.io"
  }
}

go {
  default {
    registry = "https://proxy.golang.org"
  }
}

npm {
  default {
    registry = "https://registry.npmjs.org"
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
  }
}

dist {
  gradle {
    registry = "https://services.gradle.org/distributions"
    allow = ["gradle-*-bin.zip", "gradle-*-all.zip"]
  }
}
