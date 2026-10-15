"""Build cleanup cases using real artifacts and the observed PyPI cache identity."""
from __future__ import annotations

import json
import os
import urllib.parse
from pathlib import Path
from typing import TypedDict

import psycopg


class Artifact(TypedDict):
    id: str
    ecosystem: str
    url: str
    alias: str
    repository: str
    reference: str
    group: str
    retention: str


def case(key: str, ecosystem: str, url: str, alias: str, repository: str,
         reference: str, group: str, retention: str = "expire") -> Artifact:
    return Artifact(id=key, ecosystem=ecosystem, url=url, alias=alias,
                    repository=repository, reference=reference, group=group, retention=retention)


def reference_rows(conn: psycopg.Connection, alias: str, repository: str, accept_key: str) -> list[str]:
    rows = conn.execute(
        "SELECT DISTINCT t.reference FROM meta_tags t "
        "JOIN meta_manifests m ON t.alias=m.alias AND t.repository=m.repository AND t.digest=m.digest "
        "WHERE t.alias=%s AND t.repository=%s AND m.accept_key=%s ORDER BY t.reference",
        (alias, repository, accept_key),
    ).fetchall()
    return [str(row[0]) for row in rows if row[0] is not None]


def append_maven_siblings(
    conn: psycopg.Connection, artifacts: list[Artifact], repository: str, version: str, retention: str,
) -> None:
    existing = {item["reference"] for item in artifacts if item["ecosystem"] == "maven" and item["repository"] == repository}
    for reference in reference_rows(conn, "central", repository, "maven"):
        if reference in existing:
            continue
        safe_id = "".join(char if char.isalnum() or char in "-._" else "-" for char in reference)
        artifacts.append(case(
            f"maven-{version}-extra-{safe_id}", "maven",
            f"/maven/central/{urllib.parse.quote(repository, safe='/')}/{urllib.parse.quote(reference, safe='@+')}",
            "central", repository, reference, repository, retention,
        ))


def pypi_group_key(reference: str) -> tuple[str, str]:
    artifact, _, query = reference.partition("?")
    suffixes = (".metadata", ".sha256", ".sha512", ".sha1", ".md5", ".asc", ".sig")
    while True:
        suffix = next((value for value in suffixes if artifact.endswith(value)), None)
        if suffix is None:
            return artifact, query
        artifact = artifact[:-len(suffix)]


def append_pypi_siblings(
    conn: psycopg.Connection, artifacts: list[Artifact], wheel_case: Artifact, wheel_url: str,
) -> None:
    wheel_path = wheel_case["reference"].partition("?")[0]
    wheel_group = pypi_group_key(wheel_case["reference"])
    parsed = urllib.parse.urlsplit(wheel_url)
    public_query = strip_internal_hash_query(parsed.query)
    existing = {item["reference"] for item in artifacts if item["ecosystem"] == "pypi"}
    for reference in reference_rows(conn, wheel_case["alias"], wheel_case["repository"], "pypi"):
        if reference in existing or pypi_group_key(reference) != wheel_group:
            continue
        artifact = reference.partition("?")[0]
        suffix = artifact[len(wheel_path):]
        if not suffix or suffix not in (".metadata", ".sha256", ".sha512", ".sha1", ".md5", ".asc", ".sig"):
            raise RuntimeError(f"cannot safely derive PyPI sidecar URL for {reference!r}")
        sidecar_url = urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, parsed.path + suffix, public_query, ""))
        artifacts.append(case(
            f"pypi-six-{suffix[1:]}", "pypi", sidecar_url, wheel_case["alias"],
            wheel_case["repository"], reference, wheel_case["group"], "expire",
        ))


def strip_internal_hash_query(query: str) -> str:
    parts = query.split("&") if query else []
    return "&".join(
        part for part in parts
        if urllib.parse.unquote_plus(part.partition("=")[0]) != "__regimux_sha256"
    )


def main() -> None:
    artifacts: list[Artifact] = []
    for version, suffixes, retention in (
        ("2.15.1", ("jar", "pom", "jar.sha1"), "expire"),
        ("2.16.1", ("jar", "pom"), "keep"),
    ):
        repo = f"commons-io/commons-io/{version}"
        for suffix in suffixes:
            filename = f"commons-io-{version}.{suffix}"
            artifacts.append(case(f"maven-{version}-{suffix}", "maven",
                                  f"/maven/central/{repo}/{filename}", "central",
                                  repo, filename, repo, retention))
    for version, retention in (("v1.5.2", "expire"), ("v1.5.1", "keep")):
        for suffix in ("info", "mod", "zip"):
            ref = f"@v/{version}.{suffix}"
            artifacts.append(case(f"go-{version}-{suffix}", "go", f"/go/default/rsc.io/quote/{ref}",
                                  "default", "rsc.io/quote", ref, f"rsc.io/quote@{version}", retention))
    artifacts.extend([
        case("npm-metadata", "npm", "/npm/default/lodash", "default", "lodash", "metadata", "lodash-catalog"),
        case("npm-tarball", "npm", "/npm/default/lodash/-/lodash-4.17.21.tgz", "default", "lodash",
             "tarball:lodash-4.17.21.tgz", "lodash-4.17.21"),
        case("dist-checksum", "dist", "/dist/nodejs/v20.19.0/SHASUMS256.txt", "nodejs",
             "v20.19.0", "SHASUMS256.txt", "node-shasums"),
        case("dist-archive", "dist", "/dist/nodejs/v20.19.0/node-v20.19.0-linux-x64.tar.xz", "nodejs",
             "v20.19.0", "node-v20.19.0-linux-x64.tar.xz", "node-archive"),
        case("oci-busybox", "container", "/v2/hub/library/busybox/manifests/1.36.1", "hub",
             "library/busybox", "1.36.1", "busybox-1.36.1"),
    ])
    report_dir = Path(os.getenv("REPORT_DIR", "/reports"))
    wheel: dict[str, object] = json.loads((report_dir / "pypi-wheel.json").read_text())
    wheel_url = wheel.get("url")
    if not isinstance(wheel_url, str):
        raise ValueError("PyPI client report has no valid proxy URL")
    with psycopg.connect(os.environ["PG_DSN"]) as conn:
        append_maven_siblings(conn, artifacts, "commons-io/commons-io/2.15.1", "2.15.1", "expire")
        append_maven_siblings(conn, artifacts, "commons-io/commons-io/2.16.1", "2.16.1", "keep")
        row = conn.execute(
            "SELECT t.alias,t.repository,t.reference FROM meta_tags t "
            "JOIN meta_manifests m ON t.alias=m.alias AND t.repository=m.repository AND t.digest=m.digest "
            "WHERE m.accept_key=%s AND split_part(t.reference, '?', 1) LIKE %s LIMIT 1",
            ("pypi", "%/six-1.17.0-py2.py3-none-any.whl"),
        ).fetchone()
    if row is None or not all(isinstance(value, str) for value in row):
        raise RuntimeError("six wheel has no observed PyPI cache identity")
    wheel_case = case("pypi-six", "pypi", wheel_url, str(row[0]), str(row[1]), str(row[2]), "six-wheel")
    artifacts.append(wheel_case)
    with psycopg.connect(os.environ["PG_DSN"]) as conn:
        append_pypi_siblings(conn, artifacts, wheel_case, wheel_url)
    payload = {
        "artifacts": artifacts,
        "refresh": ["maven-2.16.1-pom", "go-v1.5.1-info"],
        "synthetic_shared": {"source_id": "maven-2.15.1-jar", "alias": "acceptance-retained",
                             "repository": "shared/demo", "reference": "shared.jar", "ecosystem": "dist"},
    }
    target = report_dir / "cleanup-cases.json"
    target.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    print(f"PASS: created {len(artifacts)} real artifact cases across six ecosystems: {target}", flush=True)


if __name__ == "__main__":
    main()
