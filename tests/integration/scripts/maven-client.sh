#!/bin/sh
set -eu

concurrency="${REGIMUX_INTEGRATION_CONCURRENCY:-4}"
repository_url="${REGIMUX_MAVEN_REPOSITORY_URL:-http://regimux:8080/maven/central}"
command_timeout_seconds="${REGIMUX_MAVEN_COMMAND_TIMEOUT_SECONDS:-120}"
group_id="${REGIMUX_MAVEN_GROUP_ID:-commons-io}"
artifact_id="${REGIMUX_MAVEN_ARTIFACT_ID:-commons-io}"
version="${REGIMUX_MAVEN_VERSION:-2.16.1}"
group_path="$(printf '%s' "$group_id" | tr '.' '/')"
artifact_path="$group_path/$artifact_id/$version"
status=0

run_with_timeout() {
  if command -v timeout >/dev/null 2>&1; then
    timeout "$command_timeout_seconds" "$@"
    return
  fi
  "$@"
}

download() {
  url="$1"
  out="$2"
  run_with_timeout wget -q -O "$out" "$url"
  test -s "$out"
}

for i in $(seq 1 "$concurrency"); do
  (
    work="/tmp/maven-$i"
    rm -rf "$work"
    mkdir -p "$work"
    base_url="${repository_url%/}/$artifact_path"
    jar="$work/$artifact_id-$version.jar"
    pom="$work/$artifact_id-$version.pom"
    jar_sha1="$work/$artifact_id-$version.jar.sha1"

    echo "maven worker $i fetching ${group_id}:${artifact_id}:${version}"
    download "$base_url/$artifact_id-$version.pom" "$pom"
    download "$base_url/$artifact_id-$version.jar" "$jar"
    download "$base_url/$artifact_id-$version.jar.sha1" "$jar_sha1"

    expected="$(tr -d '[:space:]' <"$jar_sha1")"
    actual="$(sha1sum "$jar" | awk '{print $1}')"
    test "$actual" = "$expected"
    echo "maven worker $i fetched ${group_id}:${artifact_id}:${version}"
  ) &
done

for pid in $(jobs -p); do
  wait "$pid" || status=1
done

exit "$status"
