# Storage

As a dependency proxy, RegiMux separates metadata from cached artifact objects. Metadata answers questions such as which dependency path maps to which digest, when it was accessed, and how much committed cache is tracked. The object store holds the immutable bytes that package managers and container runtimes eventually reuse.

## Metadata

Metadata is stored through SQL repositories built on `dbx`. Supported drivers:

- SQLite
- MySQL
- PostgreSQL

SQLite is the default:

```hcl
store {
  meta {
    driver = "sqlite"
    path = "data/regimux.db"
  }
}
```

MySQL:

```hcl
store {
  meta {
    driver = "mysql"
    dsn = "regimux:secret@tcp(mysql:3306)/regimux?parseTime=true"
  }
}
```

PostgreSQL:

```hcl
store {
  meta {
    driver = "postgres"
    dsn = "postgres://regimux:secret@postgres:5432/regimux?sslmode=disable"
  }
}
```

Schema changes use embedded SQL migrations with per-driver migration directories.

## Object Store

Blob objects are stored outside metadata. Supported drivers:

- `local`
- `memory`
- `s3`
- `sftp`

Local filesystem is the default:

```hcl
store {
  object {
    driver = "local"
    path = "data/objects"
  }
}
```

S3-compatible storage:

```hcl
store {
  object {
    driver = "s3"

    s3 {
      bucket = "regimux-objects"
      prefix = "cache"
      region = "us-east-1"
      endpoint = "http://minio:9000"
      access_key_id = "regimux"
      secret_access_key = "change-me"
      force_path_style = true
    }
  }
}
```

SFTP:

```hcl
store {
  object {
    driver = "sftp"
    path = "/srv/regimux/objects"

    sftp {
      addr = "sftp.example.com:22"
      username = "regimux"
      password = "change-me"
      known_hosts_path = "/etc/regimux/known_hosts"
      timeout = "10s"
    }
  }
}
```

SFTP requires host key verification through `known_hosts_path` or `host_key`.

## Object Listing

Object stores expose CAS object walking where the driver supports listing. Admin storage uses this as a dry-run style reconcile signal: metadata-backed counters remain the source of truth for cache accounting, while the live object listing shows how many CAS blobs and bytes are currently visible in `store.object`.

The listing can be expensive on large or remote stores, so it is only surfaced on the storage view and may be unavailable when a driver cannot list objects or the backing service rejects the scan.

## Multi-replica Notes

For multiple RegiMux replicas, use a shared metadata store and a shared object store:

- metadata: MySQL or PostgreSQL
- objects: S3-compatible storage or SFTP
- scheduler coordination: Redis or Valkey distributed lock

Do not scale multiple replicas on independent SQLite files and local object directories unless each replica is intentionally isolated.
