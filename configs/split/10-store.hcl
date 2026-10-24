store {
  meta {
    driver = "sqlite"
    dsn = ""
    path = "data/regimux.db"
  }

  object {
    driver = "local"
    path = "data/objects"

    # The optional disk_cache block is for the S3 object driver only.
    # See ../regimux.hcl for the complete S3 and per-instance L1 example.
  }
}
