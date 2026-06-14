policy {
  dependency {
    block {
      ecosystem = "container"
      alias = "hub"
      repository = "bad-namespace/*"
    }

    block {
      ecosystem = "npm"
      alias = "default"
      repository = "left-pad"
    }
  }
}
