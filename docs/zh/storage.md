# 存储

作为依赖代理，RegiMux 将 metadata 和已缓存制品对象分开存储。metadata 负责记录依赖路径到 digest 的映射、最近访问时间和已统计缓存大小；对象存储保存包管理器和容器运行时最终复用的不可变字节。

## 元数据

元数据层基于 `dbx` repository 实现 SQL 存储。支持驱动：

- SQLite
- MySQL
- PostgreSQL

默认使用 SQLite：

```hcl
store {
  meta {
    driver = "sqlite"
    path = "data/regimux.db"
  }
}
```

MySQL：

```hcl
store {
  meta {
    driver = "mysql"
    dsn = "regimux:secret@tcp(mysql:3306)/regimux?parseTime=true"
  }
}
```

PostgreSQL：

```hcl
store {
  meta {
    driver = "postgres"
    dsn = "postgres://regimux:secret@postgres:5432/regimux?sslmode=disable"
  }
}
```

Schema 变更使用内嵌 SQL 迁移，并按数据库驱动维护独立迁移目录。

## 对象存储

blob 对象独立于元数据保存。支持驱动：

- `local`
- `memory`
- `s3`
- `sftp`

默认使用本地文件系统：

```hcl
store {
  object {
    driver = "local"
    path = "data/objects"
  }
}
```

S3 兼容存储：

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

SFTP：

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

SFTP 必须通过 `known_hosts_path` 或 `host_key` 做主机密钥校验。

## 对象枚举

对象存储在驱动支持 list 的情况下会暴露 CAS object walking。Admin 存储页把它作为类似 dry-run 的 reconcile 信号：metadata 记账仍是缓存统计的来源，而实时对象枚举显示当前 `store.object` 中可见的 CAS blob 数量和字节数。

对于大型或远端对象存储，枚举可能比较昂贵，因此只在存储页展示；当驱动无法枚举或后端拒绝扫描时，该统计会显示为不可用。

## 多副本说明

多副本部署时需要使用共享元数据和共享对象存储：

- 元数据：MySQL 或 PostgreSQL
- 对象：S3 兼容存储或 SFTP
- 调度协调：Redis 或 Valkey 分布式锁

除非有意隔离各实例，否则不要让多个副本使用各自独立的 SQLite 文件和本地对象目录。
