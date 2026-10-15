#!/bin/sh
set -eu

registry_name="${REGIMUX_REGISTRY_NAME:-regimux:8080}"
base_url="${REGIMUX_BASE_URL:-http://$registry_name}"
alias="${REGIMUX_CONTAINER_ALIAS:-}"
repository="${REGIMUX_CONTAINER_REPOSITORY:-library/busybox}"
reference="${REGIMUX_CONTAINER_REFERENCE:-1.36.1}"
docker_wait_seconds="${REGIMUX_DOCKER_WAIT_SECONDS:-120}"
command_timeout_seconds="${REGIMUX_DOCKER_COMMAND_TIMEOUT_SECONDS:-300}"

case "$registry_name" in
  ''|*[!A-Za-z0-9._:-]*) printf '%s\n' "invalid registry name: $registry_name" >&2; exit 1 ;;
esac
case "$base_url" in
  http://*|https://*) ;;
  *) printf '%s\n' "invalid RegiMux base URL: $base_url" >&2; exit 1 ;;
esac
case "$base_url" in
  *[!A-Za-z0-9:/._-]*) printf '%s\n' "unsupported character in RegiMux base URL" >&2; exit 1 ;;
esac
case "$alias" in
  ''|*[!A-Za-z0-9._-]*)
    if [ -n "$alias" ]; then
      printf '%s\n' "invalid container alias: $alias" >&2
      exit 1
    fi
    ;;
esac
case "$repository" in
  ''|/*|*/|*..*|*[!A-Za-z0-9._/-]*) printf '%s\n' "invalid container repository: $repository" >&2; exit 1 ;;
esac
case "$reference" in
  ''|*[!A-Za-z0-9._-]*) printf '%s\n' "invalid image reference: $reference" >&2; exit 1 ;;
esac
for limit in "$docker_wait_seconds" "$command_timeout_seconds"; do
  case "$limit" in
    ''|*[!0-9]*) printf '%s\n' "Docker timeouts must be positive integers" >&2; exit 1 ;;
  esac
  if [ "$limit" -le 0 ]; then
    printf '%s\n' "Docker timeouts must be positive integers" >&2
    exit 1
  fi
done

if [ -n "$alias" ]; then
  routed_repository="$alias/$repository"
else
  routed_repository="$repository"
fi
image="$registry_name/$routed_repository:$reference"
manifest_url="${base_url%/}/v2/$routed_repository/manifests/$reference"
work="$(mktemp -d "${TMPDIR:-/tmp}/regimux-oci.XXXXXX")"
trap 'docker image rm "$image" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT HUP INT TERM

waited=0
until docker info >/dev/null 2>&1; do
  if [ "$waited" -ge "$docker_wait_seconds" ]; then
    printf '%s\n' "timed out waiting for Docker-in-Docker after ${docker_wait_seconds}s" >&2
    exit 1
  fi
  sleep 1
  waited=$((waited + 1))
done

run_docker() {
  timeout "$command_timeout_seconds" docker "$@"
}

fetch_manifest_headers() {
  headers="$work/manifest.headers"
  if command -v curl >/dev/null 2>&1; then
    curl --fail --silent --show-error --dump-header "$headers" --output /dev/null \
      --header 'Accept: application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
      "$manifest_url"
  elif command -v wget >/dev/null 2>&1; then
    wget -S -O /dev/null \
      --header='Accept: application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
      "$manifest_url" 2>"$headers"
  else
    printf '%s\n' "Docker client image requires curl or wget to verify cache status" >&2
    return 1
  fi
  cache_status="$(awk 'tolower($1) ~ /^x-mirror-cache:/ {gsub("\r", "", $2); print tolower($2); exit}' "$headers")"
  if [ "$cache_status" != hit ]; then
    printf '%s\n' "expected RegiMux cache HIT after second Docker pull, got ${cache_status:-missing}" >&2
    cat "$headers" >&2
    return 1
  fi
}

run_docker image rm "$image" >/dev/null 2>&1 || true
echo "Pulling public OCI image through RegiMux: $image"
run_docker pull "$image"
run_docker image inspect "$image" >/dev/null

run_docker image rm "$image" >/dev/null
echo "Pulling it again after removing the local tag: $image"
run_docker pull "$image"
run_docker image inspect "$image" >/dev/null
fetch_manifest_headers

echo "OCI proxy acceptance passed: Docker pulled $image twice and the second manifest request was a RegiMux cache HIT."
