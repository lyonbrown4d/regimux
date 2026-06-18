#!/bin/sh
set -eu

concurrency="${REGIMUX_INTEGRATION_CONCURRENCY:-4}"
module="${REGIMUX_GO_MODULE:-github.com/gin-gonic/gin}"
version="${REGIMUX_GO_VERSION:-v1.10.1}"
proxy="${REGIMUX_GO_PROXY:-http://regimux:8080/go/default}"
module_cache_path="$(printf '%s' "$module" | tr '[:upper:]' '[:lower:]')"
status=0

for i in $(seq 1 "$concurrency"); do
  (
    work="/tmp/go-$i"
    modcache="/tmp/go-mod-cache-$i"
    rm -rf "$work" "$modcache" "/tmp/go-build-cache-$i"
    mkdir -p "$work" "$modcache"
    cd "$work"

    export GOCACHE="/tmp/go-build-cache-$i"
    export GOMODCACHE="$modcache"
    export GOPROXY="$proxy"
    export GOSUMDB=off

    go mod init "regimux.integration/client$i" >/dev/null
    go mod download -json "${module}@${version}" > download.json

    test -s "$modcache/cache/download/$module_cache_path/@v/${version}.info"
    test -s "$modcache/cache/download/$module_cache_path/@v/${version}.mod"
    test -s "$modcache/cache/download/$module_cache_path/@v/${version}.zip"
  ) &
done

for pid in $(jobs -p); do
  wait "$pid" || status=1
done

exit "$status"
