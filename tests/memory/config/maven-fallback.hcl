server {
  listen = ":8080"
  middleware {
    healthcheck { enabled = true }
  }
}
auth { enabled = false }
store {
  meta {
    driver = "sqlite"
    path = "/var/lib/regimux/meta.db"
  }
  object {
    driver = "local"
    path = "/var/lib/regimux/objects"
  }
}
scheduler { enabled = false }
maven {
  fallback {
    registry = "http://fixture:8081/maven-missing"
    mirrors = ["http://fixture:8081/maven"]
  }
  missing {
    registry = "http://fixture:8081/maven-missing"
  }
  unavailable {
    registry = "http://fixture:8081/maven-missing"
    mirrors = ["http://fixture:8081/maven-unavailable"]
  }
  denied {
    registry = "http://fixture:8081/maven-denied"
    mirrors = ["http://fixture:8081/maven"]
  }
}
