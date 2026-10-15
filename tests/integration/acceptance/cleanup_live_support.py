"""Catalog validation and HTTP helpers for live cache cleanup acceptance."""
from __future__ import annotations

import json
import os
import time
import urllib.parse
import urllib.request
from dataclasses import dataclass
from typing import Any, cast

ECOSYSTEMS = {"maven", "go", "npm", "pypi", "dist", "container"}
OLD_NS = time.time_ns() - 30 * 24 * 60 * 60 * 1_000_000_000
POLL_SECONDS = int(os.getenv("REGIMUX_ACCEPTANCE_CLEANUP_TIMEOUT", "180"))


@dataclass(frozen=True)
class Case:
    id: str
    ecosystem: str
    url: str
    alias: str
    repository: str
    reference: str
    group: str
    retention: str
    headers: dict[str, str]

    @classmethod
    def parse(cls, value: dict[str, Any]) -> Case:
        required = ("id", "ecosystem", "url", "alias", "repository", "reference", "group", "retention")
        missing = [key for key in required if not value.get(key)]
        if missing:
            raise ValueError(f"case misses {missing}: {value!r}")
        case = cls(**{key: value[key] for key in required}, headers=value.get("headers", {}))
        if case.ecosystem not in ECOSYSTEMS or case.retention not in {"expire", "keep"}:
            raise ValueError(f"invalid ecosystem/retention in case {case.id}")
        return case


def require_env(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        raise RuntimeError(f"required acceptance environment variable {name} is missing")
    return value


def load_catalog() -> tuple[list[Case], list[str], dict[str, str] | None]:
    from pathlib import Path

    catalog_path = Path(require_env("REGIMUX_ACCEPTANCE_CLEANUP_CASES_FILE"))
    data = json.loads(catalog_path.read_text(encoding="utf-8"))
    cases = [Case.parse(item) for item in data["artifacts"]]
    if len({item.id for item in cases}) != len(cases):
        raise ValueError("artifact case ids must be unique")
    if {item.ecosystem for item in cases} != ECOSYSTEMS:
        raise ValueError(f"catalog must contain all six ecosystems: {ECOSYSTEMS}")
    refresh = data.get("refresh", [])
    by_id = {item.id: item for item in cases}
    if any(key not in by_id or by_id[key].retention != "keep" for key in refresh):
        raise ValueError("refresh ids must identify keep cases")
    require_package_coverage(cases, refresh)
    return cases, refresh, data.get("synthetic_shared")


def require_package_coverage(cases: list[Case], refresh: list[str]) -> None:
    maven_expired = [c for c in cases if c.ecosystem == "maven" and c.retention == "expire"]
    maven_keep = [c for c in cases if c.ecosystem == "maven" and c.retention == "keep"]
    go_expired = [c for c in cases if c.ecosystem == "go" and c.retention == "expire"]
    go_keep = [c for c in cases if c.ecosystem == "go" and c.retention == "keep"]
    if len(maven_expired) < 3 or len(maven_keep) < 2 or len(go_expired) < 3 or len(go_keep) < 3:
        raise ValueError("catalog needs expired/kept Maven GAVs and expired/kept Go versions with sibling references")
    if len({c.group for c in maven_expired}) != 1 or len({c.group for c in maven_keep}) != 1:
        raise ValueError("Maven cases must identify one expired and one retained GAV group")
    if len({c.group for c in go_expired}) != 1 or len({c.group for c in go_keep}) != 1:
        raise ValueError("Go cases must identify one expired and one retained version group")
    for records, suffixes in ((maven_expired, (".jar", ".pom", ".sha1")), (go_expired, (".info", ".mod", ".zip"))):
        if not all(any(c.reference.endswith(suffix) for c in records) for suffix in suffixes):
            raise ValueError(f"catalog lacks required package files {suffixes}")
    for ecosystem in ECOSYSTEMS:
        if not any(c.ecosystem == ecosystem and c.retention == "expire" for c in cases):
            raise ValueError(f"catalog has no expiring {ecosystem} artifact")
    if sum(c.id in refresh for c in maven_keep) != 1 or sum(c.id in refresh for c in go_keep) != 1:
        raise ValueError("refresh must touch exactly one retained Maven and Go reference")


def request(base: str, path: str, headers: dict[str, str] | None = None) -> tuple[int, bytes | None]:
    url = urllib.parse.urljoin(base.rstrip("/") + "/", path.lstrip("/"))
    req = urllib.request.Request(url, headers=headers or {}, method="GET")
    with urllib.request.urlopen(req, timeout=180) as response:
        if not 200 <= response.status < 300:
            raise RuntimeError(f"GET {url} returned {response.status}")
        if "/v2/" in urllib.parse.urlparse(url).path and "/manifests/" in url:
            body = response.read(8 * 1024 * 1024 + 1)
            if len(body) > 8 * 1024 * 1024:
                raise RuntimeError("container manifest exceeded 8 MiB")
            return response.status, body
        while response.read(1024 * 1024):
            pass
        return response.status, None


def warm_case(base: str, case: Case) -> None:
    status, body = request(base, case.url, case.headers)
    if case.ecosystem != "container" or body is None:
        print(f"WARM {case.id} status={status}")
        return
    root = json_object(body, "container manifest")
    manifest_count, blob_count = warm_container_graph(base, case, root)
    print(f"WARM {case.id} status={status} child_manifests={manifest_count} blobs={blob_count}")


def warm_container_graph(base: str, case: Case, root: dict[str, object]) -> tuple[int, int]:
    """Fetch one preferred platform graph with strict request bounds."""
    max_manifests, max_blobs = 8, 32
    seen: set[str] = set()
    queue = preferred_children(root)
    if "manifests" in root and not queue:
        raise RuntimeError("container index has no child manifest descriptors")
    child_count = 0
    blob_digests: set[str] = set()
    while queue:
        digest = queue.pop(0)
        if digest in seen:
            continue
        if child_count + 1 >= max_manifests:
            raise RuntimeError("container manifest traversal exceeded 8 manifests")
        seen.add(digest)
        _, body = request(base, f"/v2/{case.alias}/{case.repository}/manifests/{digest}", case.headers)
        if body is None:
            raise RuntimeError(f"container child manifest {digest} returned no body")
        child_count += 1
        manifest = json_object(body, "container child manifest")
        children = preferred_children(manifest)
        if children:
            queue.extend(item for item in children if item not in seen)
        else:
            blob_digests.update(manifest_blob_digests(manifest))
    blob_digests.update(manifest_blob_digests(root))
    if len(blob_digests) > max_blobs:
        raise RuntimeError("container image graph exceeded 32 blob requests")
    for digest in sorted(blob_digests):
        request(base, f"/v2/{case.alias}/{case.repository}/blobs/{digest}")
    if "manifests" in root and child_count == 0:
        raise RuntimeError("container index did not warm a child manifest")
    return child_count, len(blob_digests)


def json_object(body: bytes, label: str) -> dict[str, object]:
    value: Any = json.loads(body)
    return object_dict(value, label)


def object_dict(value: Any, label: str) -> dict[str, object]:
    if not isinstance(value, dict):
        raise RuntimeError(f"{label} response must be an object")
    raw_dict = cast(dict[object, object], value)
    if any(not isinstance(key, str) for key in raw_dict):
        raise RuntimeError(f"{label} contains a non-string key")
    return cast(dict[str, object], raw_dict)


def preferred_children(manifest: dict[str, object]) -> list[str]:
    descriptors = manifest.get("manifests", [])
    if not isinstance(descriptors, list) or not descriptors:
        return []
    valid: list[dict[str, object]] = []
    for raw_item in cast(list[object], descriptors):
        if not isinstance(raw_item, dict):
            continue
        descriptor = object_dict(raw_item, "OCI descriptor")
        digest = descriptor.get("digest")
        if isinstance(digest, str) and digest:
            valid.append(descriptor)
    if not valid:
        raise RuntimeError("container index has no valid child descriptors")

    def is_preferred(item: dict[str, object]) -> bool:
        platform = item.get("platform")
        if not isinstance(platform, dict):
            return False
        typed_platform = object_dict(platform, "OCI platform")
        return typed_platform.get("os") == "linux" and typed_platform.get("architecture") == "amd64"

    preferred = next((item for item in valid if is_preferred(item)), valid[0])
    digest = preferred.get("digest")
    if not isinstance(digest, str):
        raise RuntimeError("selected OCI descriptor has no digest")
    return [digest]


def manifest_blob_digests(manifest: dict[str, object]) -> list[str]:
    digests: list[str] = []
    config = manifest.get("config", {})
    if isinstance(config, dict):
        digest = object_dict(config, "OCI config").get("digest")
        if isinstance(digest, str) and digest:
            digests.append(digest)
    layers = manifest.get("layers", [])
    if isinstance(layers, list):
        for item in cast(list[object], layers):
            if isinstance(item, dict):
                digest = object_dict(item, "OCI layer").get("digest")
                if isinstance(digest, str) and digest:
                    digests.append(digest)
    return digests
