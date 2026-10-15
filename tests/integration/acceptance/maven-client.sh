#!/bin/sh
set -eu

base_url="${REGIMUX_BASE_URL:-http://regimux:8080}"
repository_url="${REGIMUX_MAVEN_REPOSITORY_URL:-${base_url%/}/maven/central}"
command_timeout_seconds="${REGIMUX_MAVEN_COMMAND_TIMEOUT_SECONDS:-180}"
version="2.16.1"
artifact_path="commons-io/commons-io/$version"
artifact_name="commons-io-$version"

case "$repository_url" in
  http://*|https://*) ;;
  *) printf '%s\n' "invalid Maven repository URL: $repository_url" >&2; exit 1 ;;
esac
case "$repository_url" in
  *[!A-Za-z0-9:/._-]*) printf '%s\n' "unsupported character in Maven repository URL" >&2; exit 1 ;;
esac
case "$command_timeout_seconds" in
  ''|*[!0-9]*) printf '%s\n' "REGIMUX_MAVEN_COMMAND_TIMEOUT_SECONDS must be a positive integer" >&2; exit 1 ;;
esac
if [ "$command_timeout_seconds" -le 0 ]; then
  printf '%s\n' "REGIMUX_MAVEN_COMMAND_TIMEOUT_SECONDS must be a positive integer" >&2
  exit 1
fi

work="$(mktemp -d "${TMPDIR:-/tmp}/regimux-maven.XXXXXX")"
trap 'rm -rf "$work"' EXIT HUP INT TERM

cat >"$work/settings.xml" <<EOF
<settings xmlns="http://maven.apache.org/SETTINGS/1.2.0">
  <mirrors>
    <mirror>
      <id>regimux</id>
      <mirrorOf>*</mirrorOf>
      <url>$repository_url</url>
    </mirror>
  </mirrors>
</settings>
EOF

mkdir -p "$work/src/main/java"
cat >"$work/src/main/java/Acceptance.java" <<'EOF'
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import org.apache.commons.io.IOUtils;

public final class Acceptance {
    private Acceptance() {}

    public static void main(String[] args) throws IOException {
        String value = IOUtils.toString(
            new ByteArrayInputStream("maven-proxy".getBytes(StandardCharsets.UTF_8)),
            StandardCharsets.UTF_8
        );
        if (!"maven-proxy".equals(value)) {
            throw new IllegalStateException("unexpected Commons IO result: " + value);
        }
    }
}
EOF

cat >"$work/pom.xml" <<'EOF'
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 https://maven.apache.org/xsd/maven-4.0.0.xsd">
  <modelVersion>4.0.0</modelVersion>
  <groupId>regimux.acceptance</groupId>
  <artifactId>maven-proxy-acceptance</artifactId>
  <version>1.0</version>
  <properties>
    <maven.compiler.release>17</maven.compiler.release>
    <project.build.sourceEncoding>UTF-8</project.build.sourceEncoding>
  </properties>
  <dependencies>
    <dependency>
      <groupId>commons-io</groupId>
      <artifactId>commons-io</artifactId>
      <version>2.16.1</version>
    </dependency>
  </dependencies>
  <build>
    <plugins>
      <plugin>
        <groupId>org.apache.maven.plugins</groupId>
        <artifactId>maven-compiler-plugin</artifactId>
        <version>3.13.0</version>
      </plugin>
    </plugins>
  </build>
</project>
EOF

run_compile() {
  local_repo="$1"
  echo "Compiling against commons-io:commons-io:$version through $repository_url"
  timeout "$command_timeout_seconds" mvn -B -ntp \
    -s "$work/settings.xml" \
    -f "$work/pom.xml" \
    "-Dmaven.repo.local=$local_repo" \
    org.apache.maven.plugins:maven-compiler-plugin:3.13.0:compile
}

verify_compiled_artifacts() {
  local_repo="$1"
  artifact_dir="$local_repo/commons-io/commons-io/$version"
  test -s "$artifact_dir/$artifact_name.pom"
  test -s "$artifact_dir/$artifact_name.jar"
  test -s "$work/target/classes/Acceptance.class"
}

fetch_with_headers() {
  url="$1"
  destination="$2"
  headers="$3"
  if command -v curl >/dev/null 2>&1; then
    curl --fail --silent --show-error --dump-header "$headers" --output "$destination" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -S -O "$destination" "$url" 2>"$headers"
  else
    printf '%s\n' "Maven client image requires curl or wget to verify cached artifacts" >&2
    return 1
  fi
  test -s "$destination"
}

assert_cache_hit() {
  url="$1"
  label="$2"
  destination="$work/$label"
  headers="$work/$label.headers"
  fetch_with_headers "$url" "$destination" "$headers"
  cache_status="$(awk 'tolower($1) ~ /^x-mirror-cache:/ {gsub("\r", "", $2); print tolower($2); exit}' "$headers")"
  if [ "$cache_status" != hit ]; then
    printf '%s\n' "expected RegiMux cache HIT for $url, got ${cache_status:-missing}" >&2
    cat "$headers" >&2
    return 1
  fi
}

first_repo="$work/localrepo-first"
second_repo="$work/localrepo-second"
mkdir -p "$first_repo" "$second_repo"

run_compile "$first_repo"
verify_compiled_artifacts "$first_repo"
checksum_url="${repository_url%/}/$artifact_path/$artifact_name.jar.sha1"
fetch_with_headers "$checksum_url" "$work/first.jar.sha1" "$work/first.jar.sha1.headers"
first_expected_sha1="$(awk '{print $1}' "$work/first.jar.sha1")"
first_actual_sha1="$(sha1sum "$first_repo/commons-io/commons-io/$version/$artifact_name.jar" | awk '{print $1}')"
if [ "$first_actual_sha1" != "$first_expected_sha1" ]; then
  printf '%s\n' "first local repository JAR checksum mismatch: expected $first_expected_sha1, got $first_actual_sha1" >&2
  exit 1
fi

rm -rf "$work/target"
run_compile "$second_repo"
verify_compiled_artifacts "$second_repo"

artifact_url="${repository_url%/}/$artifact_path/$artifact_name"
assert_cache_hit "$artifact_url.pom" commons-io.pom
assert_cache_hit "$artifact_url.jar" commons-io.jar
assert_cache_hit "$artifact_url.jar.sha1" commons-io.jar.sha1

expected_sha1="$(awk '{print $1}' "$work/commons-io.jar.sha1")"
actual_sha1="$(sha1sum "$second_repo/commons-io/commons-io/$version/$artifact_name.jar" | awk '{print $1}')"
if [ "$actual_sha1" != "$expected_sha1" ]; then
  printf '%s\n' "commons-io JAR checksum mismatch: expected $expected_sha1, got $actual_sha1" >&2
  exit 1
fi

echo "Maven proxy acceptance passed: independent local repositories compiled against commons-io, verified its POM/JAR/SHA-1, and proxy returned cached POM/JAR/SHA-1."
