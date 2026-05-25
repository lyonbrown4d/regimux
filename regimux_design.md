# RegiMux：容器镜像多上游 Proxy Mirror Gateway 设计文档

版本：v0.1  
技术栈：Golang  
项目定位：自研 read-only OCI / Docker Registry V2 多上游代理缓存层  
建议项目名：**RegiMux**  
建议 daemon 名：`regimuxd`  
建议 CLI / 管理工具名：`regimuxctl`

---

## 0. 项目命名

### 0.1 推荐名称：RegiMux

**RegiMux = Registry Multiplexer**。

这个名字比较适合你的项目，因为它表达了两个核心特征：

1. 面向 container registry。
2. 把多个 upstream registry 复用到一个统一入口。

推荐命名约定：

```text
项目名：RegiMux
Git repo：regimux
服务进程：regimuxd
管理命令：regimuxctl
配置文件：regimux.yaml
镜像名：ghcr.io/<org>/regimux:<tag>
默认服务端口：5000
```

### 0.2 备选名称

| 名称 | 含义 | 评价 |
|---|---|---|
| RegiMux | Registry Multiplexer | 最推荐，技术感强，含义准确 |
| MirrorGate | Mirror Gateway | 直观，但偏泛 |
| PullMux | Pull Multiplexer | 强调 pull-through，但 registry 感弱 |
| OCIHub | OCI Hub | 好记，但容易和平台型产品混淆 |
| LayerGate | Layer Gateway | 偏底层，不能完整覆盖 manifest/tag/referrers |
| DockMux | Docker Multiplexer | 好记，但容易被误解为只支持 Docker Hub |
| Orbital | 镜像围绕多个上游轨道分发 | 品牌感强，但技术含义弱 |

本文档后续默认使用 **RegiMux** 作为项目名。

---

## 1. 背景与目标

现有 `registry:2` pull-through cache 的模型偏向“一个 registry 实例绑定一个 upstream”。这会带来几个问题：

1. 一个 upstream 就要部署一套 registry cache。
2. 多 registry 场景下部署和运维复杂。
3. 难以做统一鉴权、审计、限流、策略控制。
4. 难以做统一缓存空间管理和 GC。
5. 难以做跨 upstream 的统一观测和预热。

RegiMux 的目标是实现一个完全自研的 **OCI-aware proxy mirror gateway**。

它对外表现为一个标准 Registry HTTP API V2 服务；对内根据路径中的 upstream alias 路由到不同上游 registry，并把 manifest、blob、tags、referrers 等内容缓存到自己的 metadata store 和 blob store 中。

Registry V2 的核心 pull 接口包括：

```text
GET  /v2/
HEAD /v2/
GET  /v2/<name>/manifests/<reference>
HEAD /v2/<name>/manifests/<reference>
GET  /v2/<name>/blobs/<digest>
HEAD /v2/<name>/blobs/<digest>
GET  /v2/<name>/tags/list
```

这些接口是本项目的协议基线。

参考：

- Docker Registry HTTP API V2：<https://distribution.github.io/distribution/spec/api/>
- OCI Distribution Spec：<https://github.com/opencontainers/distribution-spec/blob/main/spec.md>

---

## 2. 设计边界

### 2.1 本期目标

本期做一个 **read-only pull-through mirror**。

目标使用方式：

```bash
docker pull mirror.example.com/hub/library/nginx:latest
docker pull mirror.example.com/ghcr/org/app:v1.2.3
docker pull mirror.example.com/quay/coreos/etcd:v3.5.0
docker pull mirror.example.com/k8s/kube-apiserver:v1.30.0
```

核心能力：

1. 单入口，多上游。
2. 支持 Docker / containerd / BuildKit / crane / oras 拉取。
3. 支持 manifest 缓存。
4. 支持 blob 内容寻址缓存。
5. 支持 tag revalidation。
6. 支持 HTTP Range 请求。
7. 支持统一鉴权、RBAC、审计、限流。
8. 支持多实例水平扩展。
9. 支持 S3 / MinIO / 本地文件系统作为 blob store。
10. 支持 PostgreSQL 作为 metadata store。
11. 支持上游 token auth、redirect、rate limit、错误码映射。
12. 支持 stale-if-error，提高上游不可用时的可用性。

### 2.2 暂不支持

第一版不支持 push registry。push 和 pull-through cache 是两个复杂度级别，不建议混在 MVP 中。

暂不支持：

1. 不支持 `docker push`。
2. 不支持 blob upload。
3. 不支持 manifest PUT。
4. 不支持 delete。
5. 不支持 image rewrite。
6. 不支持跨 registry 自动同步。
7. 不把安全扫描作为 pull 的强依赖。
8. 不默认开放 `_catalog`。
9. 不支持 Docker schema1 长期缓存。

对于 push/delete/upload 相关接口，直接返回 `405 Method Not Allowed` 或 Registry 风格的 `UNSUPPORTED` 错误。

---

## 3. 总体架构

```text
                         ┌────────────────────────────┐
                         │ Docker / containerd / CI/CD │
                         └──────────────┬─────────────┘
                                        │
                                        ▼
                         ┌────────────────────────────┐
                         │ RegiMux HTTP Server         │
                         │ Registry V2 Compatible API  │
                         └──────────────┬─────────────┘
                                        │
        ┌───────────────────────────────┼───────────────────────────────┐
        ▼                               ▼                               ▼
┌────────────────┐            ┌──────────────────┐            ┌─────────────────┐
│ AuthN / AuthZ  │            │ Reference Router │            │ Rate Limiter     │
│ token / basic  │            │ alias -> upstream│            │ user/team/upstream│
└───────┬────────┘            └─────────┬────────┘            └────────┬────────┘
        │                               │                              │
        └───────────────────────────────┼──────────────────────────────┘
                                        ▼
                         ┌────────────────────────────┐
                         │ Cache Coordinator           │
                         │ manifest/blob/tag/referrer  │
                         └──────────────┬─────────────┘
                                        │
             ┌──────────────────────────┼──────────────────────────┐
             ▼                          ▼                          ▼
┌────────────────────┐       ┌────────────────────┐       ┌────────────────────┐
│ Metadata Store      │       │ Blob Store          │       │ Upstream Client     │
│ PostgreSQL          │       │ S3/MinIO/FS         │       │ DockerHub/GHCR/etc  │
└────────────────────┘       └────────────────────┘       └────────────────────┘
             │                          │                          │
             └──────────────────────────┼──────────────────────────┘
                                        ▼
                         ┌────────────────────────────┐
                         │ Background Jobs             │
                         │ GC / prewarm / revalidate   │
                         └────────────────────────────┘
```

核心思路：

1. 对外兼容 Registry HTTP API V2。
2. 对内自己实现 upstream registry client。
3. 自己维护 metadata、object store、cache state、locks、RBAC、audit。
4. 不复用 `registry:2`，避免一 upstream 一实例的部署负担。

---

## 4. 镜像路径模型

### 4.1 推荐路径格式

推荐使用 alias-based prefix，不直接把上游域名塞进 path。

```text
mirror.example.com/hub/library/nginx:latest
mirror.example.com/ghcr/org/app:v1.2.3
mirror.example.com/quay/coreos/etcd:v3.5.0
mirror.example.com/k8s/kube-apiserver:v1.30.0
```

对应 Registry API 请求：

```http
GET /v2/hub/library/nginx/manifests/latest
GET /v2/ghcr/org/app/manifests/v1.2.3
GET /v2/quay/coreos/etcd/blobs/sha256:xxxx
```

这种格式有几个优点：

1. 路由清晰。
2. 权限策略好写。
3. 不需要处理 `registry.example.com:5000` 这种带端口的 path 编码。
4. 不暴露真实 upstream URL。
5. 以后切换 upstream 后端比较方便。

### 4.2 路由配置

```yaml
upstreams:
  hub:
    registry: https://registry-1.docker.io
    default_namespace: library
    tag_ttl: 10m
    blob_ttl: 720h
    auth:
      type: dockerhub
      username: ${DOCKERHUB_USERNAME}
      password: ${DOCKERHUB_TOKEN}

  ghcr:
    registry: https://ghcr.io
    tag_ttl: 5m
    auth:
      type: bearer
      token: ${GHCR_TOKEN}

  quay:
    registry: https://quay.io
    tag_ttl: 5m
    auth:
      type: anonymous

  k8s:
    registry: https://registry.k8s.io
    tag_ttl: 30m
    auth:
      type: anonymous
```

### 4.3 内部解析规则

以请求为例：

```http
GET /v2/hub/library/nginx/manifests/latest
```

解析结果：

```text
alias      = hub
upstream   = https://registry-1.docker.io
repo       = library/nginx
reference  = latest
operation  = get_manifest
```

内部上游请求：

```http
GET https://registry-1.docker.io/v2/library/nginx/manifests/latest
```

注意：repository name 本身可以包含多级路径，所以路径解析不要按固定下标硬切。应该先识别操作段：

```text
/manifests/
/blobs/
/tags/list
/referrers/
```

然后把 `/v2/` 到操作段之间的内容作为 name，再从 name 的第一个 segment 取 alias。

---

## 5. 协议接口设计

### 5.1 对外暴露接口

第一版支持：

```http
GET  /v2/
HEAD /v2/

GET  /v2/<alias>/<repo>/manifests/<reference>
HEAD /v2/<alias>/<repo>/manifests/<reference>

GET  /v2/<alias>/<repo>/blobs/<digest>
HEAD /v2/<alias>/<repo>/blobs/<digest>

GET  /v2/<alias>/<repo>/tags/list
GET  /v2/<alias>/<repo>/tags/list?n=<n>&last=<last>

GET  /v2/<alias>/<repo>/referrers/<digest>
```

`GET /v2/<name>/manifests/<reference>` 用于按 tag 或 digest 获取 manifest。客户端通常会带 `Accept` header 表达自己支持的 manifest media type。

`GET /v2/<name>/blobs/<digest>` 用于按 digest 拉取 layer/config/artifact blob。成功响应需要包含 blob body、`Content-Length`、`Docker-Content-Digest`、`Content-Type` 等 header。

`GET /v2/<name>/tags/list` 用于列出 repo 下的 tags，支持 `n` / `last` 分页。

OCI Distribution 1.1 增加了 referrers API：

```http
GET /v2/<name>/referrers/<digest>
```

它用于查询和某个 subject digest 关联的签名、SBOM、attestation 等 artifact。

### 5.2 暂不支持接口

```http
PUT    /v2/<name>/manifests/<reference>
DELETE /v2/<name>/manifests/<reference>

POST   /v2/<name>/blobs/uploads/
PATCH  /v2/<name>/blobs/uploads/<uuid>
PUT    /v2/<name>/blobs/uploads/<uuid>
DELETE /v2/<name>/blobs/<digest>
DELETE /v2/<name>/blobs/uploads/<uuid>
```

统一返回：

```json
{
  "errors": [
    {
      "code": "UNSUPPORTED",
      "message": "operation is unsupported by RegiMux",
      "detail": {
        "method": "PUT",
        "path": "/v2/hub/library/nginx/manifests/latest"
      }
    }
  ]
}
```

---

## 6. Manifest 缓存设计

### 6.1 Manifest 类型

需要支持：

```text
application/vnd.oci.image.manifest.v1+json
application/vnd.oci.image.index.v1+json

application/vnd.docker.distribution.manifest.v2+json
application/vnd.docker.distribution.manifest.list.v2+json
```

第一版不建议支持 Docker schema1。schema1 太老，涉及 JWS、digest 语义和兼容行为，性价比不高。

遇到 schema1 可以选择：

1. 返回 `406 Not Acceptable`。
2. 或者 pass-through，但不进入长期缓存。
3. 或者提供配置项开启兼容模式。

### 6.2 缓存 key

Manifest 缓存必须考虑 `Accept` header。

不同客户端对同一个 tag 请求时，可能因为 `Accept` 不同拿到不同 manifest representation。

建议 key：

```text
manifest_object_key = manifests/<algorithm>/<hex>
ref_cache_key       = <upstream_alias>/<repo>/<reference>/<accept_key>
```

其中：

```text
accept_key = normalized_hash(Accept header)
```

`refs` 表记录 tag/digest 引用到 manifest digest 的映射：

```text
hub/library/nginx:latest + accept_key_1 -> sha256:aaa
hub/library/nginx:latest + accept_key_2 -> sha256:bbb
```

### 6.3 tag 与 digest 的不同策略

Digest 引用：

```text
image@sha256:xxx
```

这是内容寻址引用。只要本地有对应 manifest，可以长期缓存。

Tag 引用：

```text
image:latest
image:main
image:v1
```

这是可变引用，必须定期 revalidate。

建议策略：

```yaml
cache:
  manifest:
    digest_ttl: infinite
    tag_ttl: 10m
    stale_if_error: true
    max_stale: 168h
```

### 6.4 Manifest 拉取流程

```text
1. 客户端请求 GET /v2/hub/library/nginx/manifests/latest。
2. 认证并做 RBAC 校验：repository:hub/library/nginx:pull。
3. 解析 alias/repo/reference。
4. 计算 accept_key。
5. 查询 refs 表。
6. 如果命中且未过期：
   - 读取 manifest object。
   - 返回 Content-Type / Docker-Content-Digest / Content-Length。
7. 如果未命中或过期：
   - 获取 distributed fetch lock。
   - 向 upstream 发 HEAD 或 GET。
   - 如果 HEAD 返回 digest 且本地已有同 digest manifest：
       - 更新 refs.expires_at。
       - 返回本地 manifest。
   - 否则向 upstream 发 GET。
   - 读取 raw manifest bytes。
   - 计算 digest。
   - 校验 Docker-Content-Digest。
   - 写入 object store。
   - 解析 manifest descriptors。
   - 更新 refs、manifests、manifest_links、repo_blobs。
   - 返回响应。
```

### 6.5 Manifest 响应 header

```http
HTTP/1.1 200 OK
Content-Type: application/vnd.oci.image.index.v1+json
Content-Length: <size>
Docker-Content-Digest: sha256:xxxx
Etag: "sha256:xxxx"
Cache-Control: private, max-age=...
Docker-Distribution-Api-Version: registry/2.0
X-Mirror-Cache: hit
```

注意：不要盲目信任上游 `Docker-Content-Digest`。实现上应该自己根据 body 计算 digest。

---

## 7. Blob 缓存设计

### 7.1 Blob 是内容寻址对象

Blob 包括：

1. image config。
2. layer tar gzip/zstd。
3. artifact payload。
4. SBOM/signature payload。

Blob 由 digest 唯一标识：

```text
sha256:...
```

内部存储 key：

```text
blobs/sha256/aa/aabbccddeeff...
```

### 7.2 Blob 安全边界

虽然 blob 是全局内容寻址对象，但不能只因为本地有这个 digest 就随便返回给任何 repo。

必须做 repo 级访问校验：

```text
请求：/v2/hub/private/app/blobs/sha256:xxx

校验：
1. 用户是否有 repository:hub/private/app:pull 权限。
2. repo_blobs 是否存在 hub/private/app -> sha256:xxx 关系。
3. 如果没有 repo_blobs 关系，需要向 upstream HEAD 确认该 repo 下可访问这个 blob。
```

这么做是为了避免私有镜像 blob 被另一个 repo 路径侧信道泄露。

### 7.3 Blob 状态机

```text
MISSING
  │
  │ fetch lock acquired
  ▼
FETCHING
  │
  │ digest verified
  ▼
AVAILABLE

FETCHING
  │
  │ network error / digest mismatch
  ▼
FAILED
  │
  │ backoff expired
  ▼
MISSING
```

### 7.4 Blob 拉取流程

```text
1. 客户端请求 GET /v2/hub/library/nginx/blobs/sha256:xxx。
2. 认证并做 RBAC 校验。
3. 校验 repo_blob link。
4. 查询 blobs 表。
5. 如果 status = AVAILABLE：
   - 从 blob store 读取。
   - 支持 Range。
   - 返回 200 或 206。
6. 如果未命中：
   - 获取 distributed fetch lock。
   - 向 upstream 发 GET。
   - 将内容写入对象存储。
   - 同时计算 digest。
   - digest 匹配后标记 AVAILABLE。
   - 返回给客户端。
7. 如果 digest 不匹配：
   - 删除临时对象或标记不可用。
   - 记录安全事件。
   - 返回 502 Bad Gateway。
```

### 7.5 Miss 时是否边下载边返回

#### 模式 A：cache-then-serve

```text
upstream -> RegiMux temp object -> digest verify -> client
```

优点：

1. 错误语义干净。
2. digest mismatch 时不会把坏数据发给客户端。
3. 实现简单。

缺点：

1. 首次拉取大 layer 慢。

#### 模式 B：stream-and-cache

```text
upstream -> TeeReader -> client
                  │
                  └-> temp object -> digest verify -> commit
```

优点：

1. 首次拉取延迟低。

缺点：

1. 如果最后 digest mismatch，HTTP 响应可能已经发出，只能断开连接。
2. 实现和异常处理更复杂。

建议第一版使用 **cache-then-serve**，上线稳定后再增加：

```yaml
cache:
  blob:
    stream_and_cache: true
```

### 7.6 Range 支持

本地已有 blob 时必须支持 Range。

建议第一版策略：

```text
1. blob hit：
   - 支持 Range。
   - 返回 206 Partial Content。

2. blob miss + Range 请求：
   - 先完整 fetch blob。
   - verify digest。
   - 再返回客户端请求的 range。
```

后续可以再做 sparse range cache。

---

## 8. Tags 缓存设计

Tags API 不是普通 pull 的关键路径，但很多工具会用它做镜像列表、版本选择和运维检查。

### 8.1 请求格式

```http
GET /v2/hub/library/nginx/tags/list
GET /v2/hub/library/nginx/tags/list?n=100
GET /v2/hub/library/nginx/tags/list?n=100&last=1.25
```

### 8.2 缓存 key

```text
tag_page_key = <upstream_alias>/<repo>?n=<n>&last=<last>
```

### 8.3 TTL

Tags list TTL 可以比 manifest tag TTL 稍长。

```yaml
cache:
  tags:
    ttl: 5m
    max_page_size: 1000
```

### 8.4 Link header rewrite

上游分页可能返回：

```http
Link: <https://registry-1.docker.io/v2/library/nginx/tags/list?n=100&last=1.25>; rel="next"
```

需要改写成：

```http
Link: </v2/hub/library/nginx/tags/list?n=100&last=1.25>; rel="next"
```

不要把 upstream URL 暴露给客户端。

---

## 9. Referrers 缓存设计

OCI 1.1 的 referrers API 主要服务于：

1. cosign signature。
2. notation signature。
3. SBOM。
4. provenance。
5. attestation。
6. vulnerability report。

### 9.1 请求

```http
GET /v2/ghcr/org/app/referrers/sha256:xxxx
```

转发到：

```http
GET https://ghcr.io/v2/org/app/referrers/sha256:xxxx
```

### 9.2 缓存策略

```yaml
cache:
  referrers:
    ttl: 5m
    fallback_tag: true
```

如果 upstream 返回 `404`，可以尝试 OCI fallback tag。

```text
sha256:<digest> -> sha256-<digest>
```

例如：

```http
GET /v2/org/app/manifests/sha256-21edd7d11800e94bae9f4...
```

这个 fallback 行为用于兼容不支持 Referrers API 的旧 registry。

---

## 10. Upstream Client 设计

### 10.1 技术原则

可以引入：

```text
github.com/opencontainers/go-digest
github.com/google/go-containerregistry
github.com/oras-project/oras-go
```

但热路径建议自己用 `net/http` 写 raw registry client。原因是 RegiMux 需要完整控制：

1. `Accept` header。
2. `Docker-Content-Digest`。
3. Range。
4. 307 redirect。
5. `WWW-Authenticate`。
6. stream body。
7. object store 写入。
8. 错误码映射。
9. 限流与重试。
10. telemetry。

高层库更适合做兼容性参考、测试工具或非热路径管理操作。

### 10.2 RegistryClient 接口

```go
type RegistryClient interface {
    Ping(ctx context.Context, upstream Upstream) error

    HeadManifest(ctx context.Context, req HeadManifestRequest) (*ManifestMeta, error)
    GetManifest(ctx context.Context, req GetManifestRequest) (*ManifestResponse, error)

    HeadBlob(ctx context.Context, req HeadBlobRequest) (*BlobMeta, error)
    GetBlob(ctx context.Context, req GetBlobRequest) (*BlobResponse, error)

    ListTags(ctx context.Context, req ListTagsRequest) (*TagsResponse, error)
    GetReferrers(ctx context.Context, req ReferrersRequest) (*ReferrersResponse, error)
}
```

### 10.3 Upstream token 处理

常见流程：

```text
1. 请求 upstream resource。
2. upstream 返回 401。
3. 响应里带 WWW-Authenticate。
4. client 根据 realm/service/scope 获取 Bearer token。
5. client 带 Bearer token 重试原请求。
```

内部设计：

```go
type TokenManager interface {
    GetToken(ctx context.Context, upstream Upstream, scope string) (string, error)
    Invalidate(ctx context.Context, upstream Upstream, scope string)
}
```

token cache key：

```text
<upstream_alias>/<realm>/<service>/<scope>/<credential_fingerprint>
```

支持 token 提前刷新：

```text
expires_at - jitter(10% ~ 20%)
```

### 10.4 Redirect 安全

Blob 下载可能返回 `307 Temporary Redirect` 到对象存储或 CDN。

实现要求：

1. 只允许 https redirect，除非显式配置允许 http。
2. 不把 upstream `Authorization` header 转发到不同 host。
3. 限制 redirect 次数，例如最多 5 次。
4. 记录 redirect target host，用于审计。
5. 支持按 upstream 配置 redirect allowlist。

---

## 11. 客户端认证与 RBAC

### 11.1 推荐方式：内置 Token Service

对 Docker/containerd 最兼容的方式是实现 registry token auth。

未认证访问：

```http
GET /v2/hub/library/nginx/manifests/latest
```

返回：

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="https://mirror.example.com/auth/token",service="regimux",scope="repository:hub/library/nginx:pull"
```

客户端请求 token：

```http
GET /auth/token?service=regimux&scope=repository:hub/library/nginx:pull
Authorization: Basic <base64(username:password)>
```

RegiMux 校验用户密码、LDAP、OIDC、企业 SSO 或 API token 后，签发短期 JWT：

```json
{
  "iss": "regimux",
  "sub": "user-a",
  "aud": "regimux",
  "exp": 1710000000,
  "access": [
    {
      "type": "repository",
      "name": "hub/library/nginx",
      "actions": ["pull"]
    }
  ]
}
```

### 11.2 简化方式：Basic Auth

内网 MVP 可以先支持：

```http
Authorization: Basic ...
```

直接在 gateway handler 中校验。

不过建议把 Basic Auth 只作为 token service 的登录方式，不要长期让所有 `/v2/...` 请求都直接 Basic 认证。Bearer token 更适合做 scope、过期、审计和后续 SSO 集成。

### 11.3 RBAC 资源模型

资源命名直接用 mirror path：

```text
repository:hub/library/nginx:pull
repository:ghcr/org/*:pull
repository:quay/coreos/etcd:pull
```

策略示例：

```yaml
policies:
  - subject: team-platform
    allow:
      - repository:hub/library/*:pull
      - repository:k8s/*:pull
      - repository:ghcr/company/*:pull

  - subject: team-app-a
    allow:
      - repository:ghcr/company/app-a/*:pull
      - repository:hub/library/alpine:pull
      - repository:hub/library/busybox:pull
```

---

## 12. 数据模型设计

推荐 PostgreSQL。

### 12.1 upstreams

```sql
CREATE TABLE upstreams (
    id              BIGSERIAL PRIMARY KEY,
    alias           TEXT NOT NULL UNIQUE,
    registry_url    TEXT NOT NULL,
    default_ns      TEXT,
    auth_type       TEXT NOT NULL DEFAULT 'anonymous',
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    tag_ttl_seconds INTEGER NOT NULL DEFAULT 600,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 12.2 repositories

```sql
CREATE TABLE repositories (
    id           BIGSERIAL PRIMARY KEY,
    upstream_id  BIGINT NOT NULL REFERENCES upstreams(id),
    name         TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_pull_at TIMESTAMPTZ,
    UNIQUE (upstream_id, name)
);
```

### 12.3 refs

用于 tag/digest reference 到 manifest digest 的映射。

```sql
CREATE TABLE refs (
    id               BIGSERIAL PRIMARY KEY,
    upstream_id       BIGINT NOT NULL REFERENCES upstreams(id),
    repo              TEXT NOT NULL,
    reference         TEXT NOT NULL,
    reference_type    TEXT NOT NULL, -- tag / digest
    accept_key        TEXT NOT NULL,
    manifest_digest   TEXT NOT NULL,
    media_type        TEXT NOT NULL,
    size_bytes        BIGINT NOT NULL,
    etag              TEXT,
    upstream_last_mod TIMESTAMPTZ,
    last_checked_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (upstream_id, repo, reference, accept_key)
);

CREATE INDEX idx_refs_expire ON refs (expires_at);
CREATE INDEX idx_refs_digest ON refs (manifest_digest);
```

### 12.4 manifests

```sql
CREATE TABLE manifests (
    digest          TEXT PRIMARY KEY,
    media_type      TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL,
    object_key      TEXT NOT NULL,
    subject_digest  TEXT,
    artifact_type   TEXT,
    status          TEXT NOT NULL DEFAULT 'available',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_access_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 12.5 manifest_links

记录 manifest 引用的 child manifest、config、layer、artifact blob。

```sql
CREATE TABLE manifest_links (
    manifest_digest  TEXT NOT NULL REFERENCES manifests(digest),
    child_digest     TEXT NOT NULL,
    child_media_type TEXT NOT NULL,
    child_size_bytes BIGINT,
    child_kind       TEXT NOT NULL, -- config / layer / manifest / artifact
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (manifest_digest, child_digest)
);
```

### 12.6 blobs

```sql
CREATE TABLE blobs (
    digest           TEXT PRIMARY KEY,
    size_bytes       BIGINT,
    object_key       TEXT,
    status           TEXT NOT NULL, -- fetching / available / failed
    error_message    TEXT,
    fetch_owner      TEXT,
    fetch_expires_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    committed_at     TIMESTAMPTZ,
    last_access_at   TIMESTAMPTZ
);

CREATE INDEX idx_blobs_status ON blobs (status);
CREATE INDEX idx_blobs_last_access ON blobs (last_access_at);
```

### 12.7 repo_blobs

```sql
CREATE TABLE repo_blobs (
    upstream_id     BIGINT NOT NULL REFERENCES upstreams(id),
    repo            TEXT NOT NULL,
    digest          TEXT NOT NULL,
    source_manifest TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (upstream_id, repo, digest)
);
```

### 12.8 tag_pages

```sql
CREATE TABLE tag_pages (
    upstream_id   BIGINT NOT NULL REFERENCES upstreams(id),
    repo          TEXT NOT NULL,
    n             INTEGER,
    last_tag      TEXT,
    response_json JSONB NOT NULL,
    link_header   TEXT,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (upstream_id, repo, n, last_tag)
);
```

### 12.9 fetch_locks

```sql
CREATE TABLE fetch_locks (
    lock_key    TEXT PRIMARY KEY,
    owner       TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

也可以用 Redis：

```text
SET lock_key owner NX PX 300000
```

但 PostgreSQL 版本更容易保证 metadata 状态一致。

### 12.10 audit_logs

```sql
CREATE TABLE audit_logs (
    id              BIGSERIAL PRIMARY KEY,
    request_id      TEXT NOT NULL,
    user_subject    TEXT,
    client_ip       INET,
    method          TEXT NOT NULL,
    path            TEXT NOT NULL,
    upstream_alias  TEXT,
    repo            TEXT,
    reference       TEXT,
    digest          TEXT,
    cache_status    TEXT, -- hit / miss / stale / bypass
    status_code     INTEGER,
    bytes_sent      BIGINT,
    upstream_status INTEGER,
    duration_ms     INTEGER,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

---

## 13. Blob Store 抽象

```go
type BlobStore interface {
    Stat(ctx context.Context, key string) (*ObjectInfo, error)

    Get(ctx context.Context, key string, opts GetOptions) (io.ReadCloser, *ObjectInfo, error)

    Put(ctx context.Context, key string, r io.Reader, opts PutOptions) (*ObjectInfo, error)

    Delete(ctx context.Context, key string) error
}
```

`GetOptions`：

```go
type GetOptions struct {
    Range *HTTPRange
}
```

`PutOptions`：

```go
type PutOptions struct {
    Size        int64
    ContentType string
    Metadata    map[string]string
}
```

对象 key：

```text
manifests/sha256/aa/aabbcc...
blobs/sha256/bb/bbccdd...
```

### 13.1 对象提交策略

对象存储没有真正 rename 语义，所以推荐：

```text
1. 写入 final key。
2. metadata 中 status = fetching。
3. 只有 digest 校验通过后，metadata status = available。
4. 读取时必须检查 metadata status，不允许直接按 object key 读未完成对象。
5. 校验失败则删除 object 或标记为 failed。
```

---

## 14. 并发控制与防击穿

### 14.1 本地 singleflight

进程内：

```go
var manifestGroup singleflight.Group
var blobGroup singleflight.Group
```

### 14.2 分布式 fetch lock

多实例部署时，必须有分布式锁。

Manifest lock key：

```text
manifest:<upstream_alias>:<repo>:<reference>:<accept_key>
```

Blob lock key：

```text
blob:<digest>
```

Referrers lock key：

```text
referrers:<upstream_alias>:<repo>:<digest>
```

### 14.3 等待策略

```text
1. 第一个请求拿锁。
2. 其他请求短轮询 metadata。
3. 如果对象 available，直接返回。
4. 如果 fetch failed，按退避策略决定是否重试。
5. 如果 lock 过期，新的请求可以抢锁。
```

退避：

```text
1st retry: 1s
2nd retry: 3s
3rd retry: 10s
max retry: 1m
```

---

## 15. 多上游与 fallback 设计

### 15.1 逻辑 upstream 与物理 endpoint

建议区分：

```text
logical upstream: hub
physical endpoints:
  - https://registry-1.docker.io
  - https://mirror.gcr.io
```

配置：

```yaml
upstreams:
  hub:
    registry: https://registry-1.docker.io
    remotes:
      - url: https://registry-1.docker.io
        role: primary
      - url: https://mirror.gcr.io
        role: fallback
        allow_tag_resolution: false
        allow_blob_fetch: true
```

### 15.2 fallback 原则

对于 tag：

```text
默认只从 primary resolve tag。
```

原因：tag 是可变引用，不同 registry 的 tag 可能不同步。

对于 digest：

```text
可以从 fallback 拉 blob 或 digest manifest。
```

原因：digest 是内容寻址，只要下载后本地校验 digest，就能保证内容正确。

策略：

```text
1. tag manifest resolution：primary only。
2. digest manifest fetch：primary -> fallback。
3. blob fetch：primary -> fallback。
4. tag fallback 需要显式开启。
```

---

## 16. 错误处理

统一返回 Docker Registry 风格错误结构：

```json
{
  "errors": [
    {
      "code": "MANIFEST_UNKNOWN",
      "message": "manifest unknown",
      "detail": {
        "repo": "hub/library/nginx",
        "reference": "not-exist"
      }
    }
  ]
}
```

### 16.1 错误映射

```text
upstream 401 -> UNAUTHORIZED
upstream 403 -> DENIED
upstream 404 manifest -> MANIFEST_UNKNOWN
upstream 404 blob -> BLOB_UNKNOWN
upstream 429 -> TOOMANYREQUESTS
upstream 5xx -> UPSTREAM_ERROR / 502
digest mismatch -> DIGEST_INVALID / 502
unsupported method -> UNSUPPORTED / 405
invalid path -> NAME_INVALID / 400
invalid digest -> DIGEST_INVALID / 400
```

### 16.2 stale-if-error

当 upstream 不可用时：

```text
1. digest pull：如果本地有，直接返回。
2. tag pull：如果 ref 已过期但本地有旧 manifest，可以按配置返回 stale。
3. blob pull：如果本地有，直接返回。
```

配置：

```yaml
cache:
  stale_if_error: true
  max_stale: 168h
```

响应 header：

```http
Warning: 110 - "Response is stale"
X-Mirror-Cache: stale
```

---

## 17. 安全设计

### 17.1 防缓存污染

必须做：

1. digest request 必须本地校验 body。
2. blob commit 前必须校验 digest。
3. manifest 存储前计算 digest。
4. 不缓存 401/403/5xx 作为成功结果。
5. 不信任上游 `Docker-Content-Digest` 作为唯一依据。

### 17.2 防私有 blob 泄露

```text
1. 用户必须有 repo pull 权限。
2. blob 返回前检查 repo_blobs。
3. repo_blobs 不存在时，必须 upstream HEAD 验证该 repo 下 blob 可访问。
4. 不提供 “digest exists” 类公开查询接口。
```

### 17.3 上游凭证隔离

```text
1. mirror 用户 token 不直接转发给 upstream。
2. upstream credential 按 alias 隔离。
3. credential 存储加密。
4. 日志中永不打印 Authorization。
5. redirect 到不同 host 时不携带 Authorization。
```

### 17.4 Path 安全

```text
1. 禁止 ..。
2. 禁止空 repo segment。
3. 严格校验 digest 格式。
4. 严格限制 alias 必须存在于配置。
5. 不允许客户端通过 path 构造任意 upstream URL。
```

### 17.5 读写边界

```text
1. /v2 下只允许 pull 相关方法。
2. admin API 单独域名或单独 auth。
3. 默认不开放 _catalog。
4. push 相关接口全部关闭。
```

---

## 18. GC 设计

GC 分两类：

```text
1. metadata GC
2. blob object GC
```

### 18.1 保留策略

```yaml
gc:
  manifest_retention: 30d
  blob_retention: 30d
  failed_retention: 24h
  min_free_space: 100Gi
  max_storage_bytes: 5Ti
```

### 18.2 mark-sweep 流程

```text
1. 标记所有未过期 refs 指向的 manifest。
2. 递归标记 manifest_links 中的 child manifest。
3. 标记 manifest 引用的 config/layer/artifact blob。
4. 标记最近 N 天访问过的 blobs。
5. 未被标记且超过 grace period 的对象进入删除候选。
6. 先删 object store，再删 metadata。
7. 删除失败保留 tombstone，后续重试。
```

### 18.3 GC 并发安全

```text
1. 不删除 status = fetching 的对象。
2. 不删除 fetch lock 未过期的对象。
3. 删除前二次检查 last_access_at。
4. 删除前检查 ref/manifest_links/repo_blobs 是否仍引用。
```

---

## 19. 预热设计

预热 API：

```http
POST /admin/prewarm
Content-Type: application/json
```

请求：

```json
{
  "images": [
    "hub/library/nginx:latest",
    "hub/library/alpine:3.20",
    "ghcr/company/app:v1.2.3"
  ],
  "platforms": [
    "linux/amd64",
    "linux/arm64"
  ],
  "fetch_blobs": true
}
```

预热流程：

```text
1. resolve manifest。
2. 如果是 index/manifest list，按 platforms 选择 child manifest。
3. 拉取 child manifest。
4. 拉取 config blob。
5. 拉取 layer blobs。
6. 记录结果。
```

第一版可以只做 lazy cache，预热作为后台 job 后置。

---

## 20. 可观测性

### 20.1 Metrics

Prometheus 指标：

```text
regimux_http_requests_total{method,path,status}
regimux_http_request_duration_seconds_bucket

regimux_cache_hits_total{type,upstream}
regimux_cache_misses_total{type,upstream}
regimux_cache_stale_total{type,upstream}

regimux_upstream_requests_total{upstream,method,status}
regimux_upstream_request_duration_seconds_bucket
regimux_upstream_bytes_total{upstream,type}

regimux_blob_fetch_total{upstream,status}
regimux_blob_fetch_bytes_total{upstream}
regimux_blob_fetch_duration_seconds_bucket

regimux_manifest_revalidate_total{upstream,status}

regimux_storage_bytes{type}
regimux_storage_objects{type}

regimux_auth_denied_total{reason}
regimux_rate_limited_total{scope}
```

### 20.2 日志字段

```json
{
  "request_id": "req-xxx",
  "user": "team-a-ci",
  "client_ip": "10.0.1.2",
  "method": "GET",
  "path": "/v2/hub/library/nginx/manifests/latest",
  "upstream": "hub",
  "repo": "library/nginx",
  "reference": "latest",
  "cache": "hit",
  "status": 200,
  "bytes": 1234,
  "duration_ms": 12
}
```

### 20.3 Tracing

建议接入 OpenTelemetry：

```text
span: http.request
span: auth.verify
span: route.resolve
span: cache.lookup
span: fetch.lock
span: upstream.request
span: blobstore.get / blobstore.put
span: db.query
```

---

## 21. Go 项目结构

```text
regimux/
  cmd/
    regimuxd/
      main.go
    regimuxctl/
      main.go

  internal/
    api/
      server.go
      middleware.go
      routes.go
      handlers_v2.go
      error_response.go

    auth/
      token_service.go
      jwt.go
      basic.go
      middleware.go

    policy/
      rbac.go
      matcher.go

    reference/
      parser.go
      digest.go
      accept.go
      range.go

    upstream/
      client.go
      auth.go
      token_manager.go
      redirect.go
      errors.go

    cache/
      manifest_service.go
      blob_service.go
      tag_service.go
      referrer_service.go
      coordinator.go
      locks.go

    store/
      meta/
        postgres.go
        migrations/
      object/
        interface.go
        s3.go
        fs.go

    gc/
      marker.go
      sweeper.go

    jobs/
      prewarm.go
      revalidate.go

    observability/
      metrics.go
      tracing.go
      logging.go

    config/
      config.go
      load.go

  pkg/
    distribution/
      media_types.go
      errors.go

  deploy/
    docker-compose.yaml
    helm/

  docs/
    design.md

  go.mod
```

---

## 22. 核心接口草案

### 22.1 ManifestService

```go
type ManifestService interface {
    Get(ctx context.Context, req ManifestRequest) (*CachedManifest, error)
    Head(ctx context.Context, req ManifestRequest) (*ManifestMeta, error)
}

type ManifestRequest struct {
    UpstreamAlias string
    Repo          string
    Reference     string
    Accept        string
    User          *UserContext
}

type CachedManifest struct {
    Digest    string
    MediaType string
    Size      int64
    Body      []byte
    Headers   http.Header
    Cache     CacheStatus
}
```

### 22.2 BlobService

```go
type BlobService interface {
    Get(ctx context.Context, req BlobRequest) (*BlobReadResult, error)
    Head(ctx context.Context, req BlobRequest) (*BlobMeta, error)
}

type BlobRequest struct {
    UpstreamAlias string
    Repo          string
    Digest        string
    Range         *HTTPRange
    User          *UserContext
}

type BlobReadResult struct {
    Reader    io.ReadCloser
    Digest    string
    Size      int64
    Range     *HTTPRange
    Status    int
    Headers   http.Header
    Cache     CacheStatus
}
```

### 22.3 ObjectStore

```go
type ObjectStore interface {
    Stat(ctx context.Context, key string) (*ObjectInfo, error)
    Get(ctx context.Context, key string, opts GetOptions) (io.ReadCloser, *ObjectInfo, error)
    Put(ctx context.Context, key string, r io.Reader, opts PutOptions) (*ObjectInfo, error)
    Delete(ctx context.Context, key string) error
}
```

### 22.4 LockManager

```go
type LockManager interface {
    TryLock(ctx context.Context, key string, ttl time.Duration) (*Lock, bool, error)
    Unlock(ctx context.Context, lock *Lock) error
    Refresh(ctx context.Context, lock *Lock, ttl time.Duration) error
}
```

---

## 23. Handler 伪代码

### 23.1 GetManifest

```go
func (h *Handler) GetManifest(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    route, err := reference.ParseManifestPath(r.URL.Path)
    if err != nil {
        WriteError(w, ErrNameInvalid(err))
        return
    }

    user, err := h.auth.RequirePull(ctx, r, route.MirrorRepo())
    if err != nil {
        h.auth.WriteChallenge(w, r, route.MirrorRepo())
        return
    }

    result, err := h.manifests.Get(ctx, ManifestRequest{
        UpstreamAlias: route.Alias,
        Repo:          route.Repo,
        Reference:     route.Reference,
        Accept:        r.Header.Get("Accept"),
        User:          user,
    })
    if err != nil {
        WriteDistributionError(w, err)
        return
    }

    headers := w.Header()
    headers.Set("Content-Type", result.MediaType)
    headers.Set("Content-Length", strconv.FormatInt(result.Size, 10))
    headers.Set("Docker-Content-Digest", result.Digest)
    headers.Set("Docker-Distribution-Api-Version", "registry/2.0")
    headers.Set("X-Mirror-Cache", result.Cache.String())

    w.WriteHeader(http.StatusOK)
    _, _ = w.Write(result.Body)
}
```

### 23.2 GetBlob

```go
func (h *Handler) GetBlob(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    route, err := reference.ParseBlobPath(r.URL.Path)
    if err != nil {
        WriteError(w, ErrNameInvalid(err))
        return
    }

    user, err := h.auth.RequirePull(ctx, r, route.MirrorRepo())
    if err != nil {
        h.auth.WriteChallenge(w, r, route.MirrorRepo())
        return
    }

    httpRange, err := reference.ParseRange(r.Header.Get("Range"))
    if err != nil {
        WriteError(w, ErrRangeInvalid(err))
        return
    }

    result, err := h.blobs.Get(ctx, BlobRequest{
        UpstreamAlias: route.Alias,
        Repo:          route.Repo,
        Digest:        route.Digest,
        Range:         httpRange,
        User:          user,
    })
    if err != nil {
        WriteDistributionError(w, err)
        return
    }
    defer result.Reader.Close()

    headers := w.Header()
    headers.Set("Content-Type", "application/octet-stream")
    headers.Set("Docker-Content-Digest", result.Digest)
    headers.Set("Accept-Ranges", "bytes")
    headers.Set("X-Mirror-Cache", result.Cache.String())

    if result.Range != nil {
        headers.Set("Content-Range", result.Range.ContentRange(result.Size))
        headers.Set("Content-Length", strconv.FormatInt(result.Range.Length(), 10))
        w.WriteHeader(http.StatusPartialContent)
    } else {
        headers.Set("Content-Length", strconv.FormatInt(result.Size, 10))
        w.WriteHeader(http.StatusOK)
    }

    _, _ = io.Copy(w, result.Reader)
}
```

---

## 24. 部署架构

### 24.1 单机开发

```text
regimuxd
postgres
minio
```

### 24.2 生产高可用

```text
              ┌──────────────┐
              │ Load Balancer │
              └───────┬──────┘
                      │
        ┌─────────────┼─────────────┐
        ▼             ▼             ▼
   regimuxd-1     regimuxd-2     regimuxd-3
        │             │             │
        └─────────────┼─────────────┘
                      ▼
              PostgreSQL HA
                      │
                      ▼
                S3 / MinIO
```

所有 `regimuxd` 实例尽量无状态。

状态放在：

1. PostgreSQL：metadata、locks、audit。
2. S3/MinIO：manifest object、blob object。
3. Redis：可选，用于限流、短期 lock、token cache。

---

## 25. 配置文件示例

```yaml
server:
  listen: ":5000"
  public_url: "https://mirror.example.com"
  read_timeout: 30s
  write_timeout: 0s
  idle_timeout: 120s
  max_body_size: 0

auth:
  mode: token
  issuer: regimux
  service: regimux
  token_ttl: 15m
  private_key_file: /etc/regimux/jwt.key
  users:
    provider: oidc
    oidc:
      issuer_url: https://sso.example.com
      client_id: regimux

policy:
  file: /etc/regimux/policy.yaml

database:
  driver: postgres
  dsn: postgres://regimux:regimux@postgres:5432/regimux?sslmode=require

object_store:
  type: s3
  bucket: regimux-cache
  region: ap-northeast-1
  endpoint: https://s3.example.com
  access_key: ${S3_ACCESS_KEY}
  secret_key: ${S3_SECRET_KEY}

cache:
  manifest:
    tag_ttl: 10m
    stale_if_error: true
    max_stale: 168h
  blob:
    stream_and_cache: false
  tags:
    ttl: 5m
    max_page_size: 1000
  referrers:
    ttl: 5m
    fallback_tag: true

upstreams:
  hub:
    registry: https://registry-1.docker.io
    default_namespace: library
    auth:
      type: basic
      username: ${DOCKERHUB_USERNAME}
      password: ${DOCKERHUB_TOKEN}
    remotes:
      - url: https://registry-1.docker.io
        role: primary

  ghcr:
    registry: https://ghcr.io
    auth:
      type: bearer
      token: ${GHCR_TOKEN}

  quay:
    registry: https://quay.io
    auth:
      type: anonymous

gc:
  enabled: true
  interval: 6h
  manifest_retention: 30d
  blob_retention: 30d
  failed_retention: 24h
  grace_period: 24h

observability:
  metrics:
    enabled: true
    path: /metrics
  tracing:
    enabled: true
    endpoint: http://otel-collector:4317
```

---

## 26. MVP 实现路线

### Phase 1：协议骨架

目标：能被 Docker/containerd 当成 registry 访问。

```text
1. 实现 /v2/。
2. 实现路径 parser。
3. 实现 GET/HEAD manifest 纯 proxy。
4. 实现 GET/HEAD blob 纯 proxy。
5. 实现 Distribution 风格错误返回。
6. 用 docker pull 跑通 hub/library/alpine。
```

验收：

```bash
docker pull mirror.example.com/hub/library/alpine:latest
docker pull mirror.example.com/hub/library/nginx:latest
```

### Phase 2：Manifest 缓存

```text
1. PostgreSQL metadata。
2. ObjectStore manifest put/get。
3. tag TTL。
4. Accept header cache key。
5. HEAD revalidation。
6. stale-if-error。
```

验收：

```text
1. 第一次拉取 manifest miss。
2. 第二次拉取 manifest hit。
3. tag 过期后能 revalidate。
4. upstream 断网时 digest pull 仍可命中。
```

### Phase 3：Blob 缓存

```text
1. blob object store。
2. digest 校验。
3. repo_blob link。
4. singleflight。
5. distributed lock。
6. Range hit 支持。
```

验收：

```text
1. 第一次拉取 layer miss。
2. 第二次拉取 layer hit。
3. 并发 100 个相同 layer 只产生 1 个 upstream fetch。
4. digest mismatch 不进入缓存。
```

### Phase 4：认证与审计

```text
1. token service。
2. JWT access token。
3. RBAC policy。
4. audit log。
5. rate limit。
```

验收：

```text
1. 未登录 docker pull 返回 401 challenge。
2. docker login 后可以 pull。
3. 无权限 repo 返回 403。
4. audit_logs 可查每次拉取。
```

### Phase 5：Tags、Referrers、GC

```text
1. tags/list 缓存。
2. Link header rewrite。
3. referrers API。
4. fallback tag。
5. mark-sweep GC。
```

### Phase 6：生产化

```text
1. HA 部署。
2. Prometheus metrics。
3. OpenTelemetry tracing。
4. admin prewarm。
5. upstream health check。
6. chaos test。
```

---

## 27. 测试方案

### 27.1 单元测试

```text
1. path parser。
2. Accept header normalize。
3. digest parser。
4. Range parser。
5. WWW-Authenticate parser。
6. RBAC matcher。
7. error mapping。
```

### 27.2 集成测试

使用真实 registry 或本地测试 registry：

```text
1. docker.io public image。
2. ghcr.io public image。
3. quay.io public image。
4. private upstream。
5. multi-arch image。
6. large layer image。
7. tag update。
8. upstream 401/403/404/429/5xx。
```

### 27.3 客户端兼容测试

```bash
docker pull mirror.example.com/hub/library/alpine:latest

ctr image pull mirror.example.com/hub/library/alpine:latest

crane manifest mirror.example.com/hub/library/alpine:latest

oras manifest fetch mirror.example.com/ghcr/org/artifact:v1
```

### 27.4 压测

重点压：

```text
1. 同一 image 高并发。
2. 同一 blob 高并发。
3. 多 repo 混合拉取。
4. 大 layer。
5. upstream 慢响应。
6. object store 慢响应。
7. DB lock 竞争。
```

---

## 28. 关键风险与建议

### 28.1 Accept header 是第一大坑

同一个 tag 在不同 `Accept` 下可能返回不同 manifest 类型。不要只用：

```text
repo + tag
```

做缓存 key。至少要把 `Accept` 归一化后加入 ref key。

### 28.2 Blob 不能只按 digest 放行

内部可以全局 CAS，但对外返回前必须校验 repo 访问关系。否则 private blob 可能通过 digest 被探测或下载。

### 28.3 首版不要支持 push

push 涉及 resumable upload、cross-repo mount、manifest validation、delete、GC 引用一致性，和 pull-through mirror 是两个复杂度级别。先把 read-only mirror 做稳。

### 28.4 Tag fallback 要保守

digest fallback 安全，tag fallback 不一定安全。因为 tag 是可变引用，fallback registry 可能不是同一时刻的内容。

### 28.5 Object store commit 要靠 metadata 状态保护

不要因为对象已经写到了 final key 就允许读取。必须以 metadata `status = available` 为准。

### 28.6 不要让上游凭证越权

如果上游使用 service account，RegiMux 必须自己做 RBAC。不然 service account 能访问的私有镜像，会被所有能访问 RegiMux 的用户间接访问。

### 28.7 不要忽略 redirect header 泄露

上游 blob 下载可能 redirect 到 CDN。跳转到不同 host 时，不要携带原始 Authorization header。

---

## 29. 最终推荐方案

RegiMux 应该拆成三层：

```text
第一层：Registry-compatible HTTP API
  - 对 Docker/containerd 表现得像标准 registry。

第二层：OCI-aware cache coordinator
  - 懂 manifest、blob、tag、digest、referrers。
  - 控制 TTL、revalidate、singleflight、GC。

第三层：Multi-upstream registry client
  - 懂 DockerHub/GHCR/Quay/registry.k8s.io 的认证差异。
  - 支持 fallback、redirect、限流、审计。
```

第一版最小闭环：

```text
GET/HEAD manifest
GET/HEAD blob
GET tags/list
token auth
PostgreSQL metadata
S3/MinIO blob store
singleflight + distributed lock
digest verification
```

这条路线比魔改 `registry:2` 更干净，也比多个 `registry:2` 后端加 gateway 部署简单。代价是你要认真处理协议细节，尤其是：

1. `Accept`。
2. `Docker-Content-Digest`。
3. Range。
4. token auth。
5. tag revalidation。
6. private blob 泄露边界。
7. 307 redirect 安全。
8. stale-if-error 的语义。
9. distributed lock 和 cache stampede。
10. metadata 与 object store 的一致性。

---

## 30. 参考资料

- Docker Registry HTTP API V2：<https://distribution.github.io/distribution/spec/api/>
- Docker Registry Token Authentication：<https://distribution.github.io/distribution/spec/auth/token/>
- Docker Registry Scope Documentation：<https://distribution.github.io/distribution/spec/auth/scope/>
- OCI Distribution Spec：<https://github.com/opencontainers/distribution-spec/blob/main/spec.md>
- OCI Image Spec：<https://github.com/opencontainers/image-spec>
- OCI Image and Distribution 1.1 announcement：<https://opencontainers.org/posts/blog/2024-03-13-image-and-distribution-1-1/>
- opencontainers/go-digest：<https://github.com/opencontainers/go-digest>
- google/go-containerregistry：<https://github.com/google/go-containerregistry>
- oras-go：<https://github.com/oras-project/oras-go>
