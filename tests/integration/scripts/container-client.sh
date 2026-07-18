#!/bin/sh
set -eu

until docker info >/dev/null 2>&1; do
  sleep 1
done

concurrency="${REGIMUX_INTEGRATION_CONCURRENCY:-4}"
images="${REGIMUX_CONTAINER_IMAGES:-regimux:8080/library/busybox:1.36.1 regimux:8080/hub/library/busybox:1.36.1 regimux:8080/moby/buildkit:buildx-stable-1}"

set -- $images
if [ "$#" -eq 0 ]; then
  printf '%s\n' "REGIMUX_CONTAINER_IMAGES must contain at least one image" >&2
  exit 1
fi

for image in "$@"; do
  case "$image" in
    regimux:8080/*) ;;
    *)
      printf '%s\n' "container integration images must use the RegiMux proxy path: $image" >&2
      exit 1
      ;;
  esac

  docker image rm "$image" >/dev/null 2>&1 || true
done

status=0
pull_index=1

while [ "$pull_index" -le "$concurrency" ]; do
  for image in "$@"; do
    [ "$pull_index" -le "$concurrency" ] || break
    (
      docker pull "$image" > "/tmp/regimux-pull-$pull_index.log" 2>&1
    ) &
    pull_index=$((pull_index + 1))
  done
done

for pid in $(jobs -p); do
  wait "$pid" || status=1
done

if [ "$status" -ne 0 ]; then
  cat /tmp/regimux-pull-*.log
  exit "$status"
fi

for image in "$@"; do
  docker image inspect "$image" >/dev/null
  docker image rm "$image" >/dev/null 2>&1 || true
done

for image in "$@"; do
  docker pull "$image" > "/tmp/regimux-second-pull.log" 2>&1 || {
    cat "/tmp/regimux-second-pull.log"
    exit 1
  }
  docker image inspect "$image" >/dev/null
  docker image rm "$image" >/dev/null 2>&1 || true

  repository_with_ref="${image#regimux:8080/}"
  repository="${repository_with_ref%:*}"
  reference="${repository_with_ref##*:}"
  manifest_url="http://regimux:8080/v2/${repository}/manifests/${reference}"
  cache_status="$(
    wget -S --spider \
      --header="Accept: application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json" \
      "$manifest_url" \
      2>&1 |
      awk 'tolower($0) ~ /x-mirror-cache:/ {print tolower($2); exit}' |
      tr -d '\r'
  )"

  if [ "$cache_status" != "hit" ]; then
    printf '%s\n' "expected second pull manifest cache hit for $image, got ${cache_status:-missing}" >&2
    exit 1
  fi
done
