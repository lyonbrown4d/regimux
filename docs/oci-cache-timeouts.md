# OCI cache timeouts

`cache.blob.open_timeout` controls how long each OCI cached-object `Get` may take to return a reader. It applies to the initial full or Range read and to every continuation reopen after an interrupted cached read. The default is `2s`, preserving the previous behavior. An explicit zero uses that default; negative values fail configuration validation.

For an object store whose successful opens need a larger budget, set a positive duration:

```hcl
cache {
  blob {
    open_timeout = "5s"
    stream_and_cache = true
  }
}
```

The equivalent environment variable is `REGIMUX_CACHE__BLOB__OPEN_TIMEOUT=5s`. The public JSON field is `cache.blob.open_timeout`; JSON serialization represents `time.Duration` as nanoseconds. This setting applies to OCI blob cache reads. It does not change the transfer defaults used by other ecosystems.

## Which operation a timeout covers

| Operation | Budget and behavior |
| --- | --- |
| Initial cached-object `Get` | `cache.blob.open_timeout` covers opening the reader. It does not require the complete artifact to arrive within that duration. |
| Cached continuation reopen | Each reopen receives the same configured budget, retains the original digest and response bounds, and requests only the remaining bytes. Recovery permits at most three continuation attempts. |
| Cached body read | A separate 10-second idle budget starts when consumption begins. Progress renews the budget; pauses and active reads are bounded. This is not a total download deadline or a strict maximum interval between every pair of delivered bytes. |
| Metadata lookup, `Stat`, and `Exists` | These use their own backend behavior and the caller context. They are outside `cache.blob.open_timeout`, including checks performed before opening a cached body. |
| Upstream membership verification | If `cache.blob.verify_ttl` requires verification, an upstream HEAD can run before serving a cached blob. Its upstream request budget is separate. Verification is disabled by default. |
| Upstream blob response opening | The upstream's `http.attempt_timeout` covers the work inside each `requestBlobAttempt`, including authentication and request retries, until a response is opened. Continuation requests receive their own attempt budget. This is separate from opening an object-store cache reader. |
| Upstream HTTP request and blob body | A positive upstream `http.timeout` supplies the blob body idle budget. If the caller context has no deadline, Resty also adds a fixed HTTP request-lifetime deadline that remains active while the body is consumed, even if bytes keep arriving. If the caller already has a deadline, Resty does not add that client deadline. Zero disables the client fixed deadline and uses the 10-second default for body idle. Changing the cached-object open budget does not change these behaviors. |
| Caller cancellation or deadline | The caller can end the request earlier. A larger cache-open budget does not extend the caller's deadline. |

The object-open timer cancels the attempt context. The backend must honor cancellation for the open to return promptly; a blocked filesystem operation is not made interruptible by this setting. Successful opens stop that timer, allowing a progressing body to continue under the separate read budget. Each continuation uses the original configured open and idle budgets.

## Fallback and disk L1 boundaries

An initial stored-object body-open error can trigger an upstream fallback. The four shared fallback slots limit those fallback transfers through body completion, close, or cancellation. They do not limit concurrent cache `Get` calls, continuation reopens, or the number of callers waiting for a fallback slot. Waiting for a slot uses the caller context. Metadata, membership, and `Stat` failures do not enter this body-open fallback path; an object-not-found result follows the ordinary cache-miss path.

After a cached response has started streaming, continuation recovery reopens the same object store. It does not switch that response to the upstream fallback. If the upstream is unavailable, an initial fallback can also fail even when the original cache open merely needed more time. Choose a larger open budget from observed successful cache-open latency, while accounting for the caller's deadline.

The S3 [disk L1](object-disk-cache.md) caches object bodies. Canonical S3 metadata remains authoritative for `Stat`, `Exists`, `List`, and `Walk`, so a warm local body does not remove those earlier network checks. A warm Range read can use the local file; a cold Range read goes to S3 without filling a complete L1 object. Eligible concurrent full reads can share one streamed fill, with independent readers and cancellation. The L1 fill limit and the four upstream fallback slots govern different work.
