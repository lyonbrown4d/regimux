#!/bin/sh
set -eu

concurrency="${REGIMUX_INTEGRATION_CONCURRENCY:-8}"
repository_url="${REGIMUX_MAVEN_REPOSITORY_URL:-http://regimux:8080/maven/central}"
dependency_plugin_version="${REGIMUX_MAVEN_DEPENDENCY_PLUGIN_VERSION:-3.7.1}"
group_id="${REGIMUX_MAVEN_GROUP_ID:-commons-io}"
artifact_id="${REGIMUX_MAVEN_ARTIFACT_ID:-commons-io}"
version="${REGIMUX_MAVEN_VERSION:-2.16.1}"
group_path="$(printf '%s' "$group_id" | tr '.' '/')"
settings="/tmp/regimux-maven-settings.xml"
status=0

cat >"$settings" <<EOF
<settings xmlns="http://maven.apache.org/SETTINGS/1.2.0"
          xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
          xsi:schemaLocation="http://maven.apache.org/SETTINGS/1.2.0 https://maven.apache.org/xsd/settings-1.2.0.xsd">
  <profiles>
    <profile>
      <id>regimux-integration</id>
      <repositories>
        <repository>
          <id>regimux-central</id>
          <url>${repository_url}</url>
          <releases>
            <enabled>true</enabled>
          </releases>
          <snapshots>
            <enabled>false</enabled>
          </snapshots>
        </repository>
      </repositories>
      <pluginRepositories>
        <pluginRepository>
          <id>central</id>
          <url>https://repo.maven.apache.org/maven2</url>
          <releases>
            <enabled>true</enabled>
          </releases>
          <snapshots>
            <enabled>false</enabled>
          </snapshots>
        </pluginRepository>
      </pluginRepositories>
    </profile>
  </profiles>
  <activeProfiles>
    <activeProfile>regimux-integration</activeProfile>
  </activeProfiles>
</settings>
EOF

for i in $(seq 1 "$concurrency"); do
  (
    work="/tmp/maven-$i"
    rm -rf "$work"
    mkdir -p "$work/repository"
    mvn -B -ntp -q \
      -s "$settings" \
      -Dmaven.repo.local="$work/repository" \
      "org.apache.maven.plugins:maven-dependency-plugin:${dependency_plugin_version}:get" \
      -Dartifact="${group_id}:${artifact_id}:${version}:jar" \
      -Dtransitive=false \
      -DremoteRepositories="regimux-central::default::${repository_url}"
    test -s "$work/repository/$group_path/$artifact_id/$version/${artifact_id}-${version}.jar"
  ) &
done

for pid in $(jobs -p); do
  wait "$pid" || status=1
done

exit "$status"
