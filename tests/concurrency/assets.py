"""Small deterministic package artifacts used by the offline HTTP fixture."""
from __future__ import annotations

import base64
import csv
import gzip
import hashlib
import io
import json
import lzma
import tarfile
import zipfile
from functools import lru_cache
from typing import NamedTuple


VERSION = "1.0.0"


class Artifact(NamedTuple):
    body: bytes
    content_type: str
    headers: dict[str, str]


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def tar_bytes(entries: dict[str, bytes], mode: str = "w") -> bytes:
    target = io.BytesIO()
    with tarfile.open(fileobj=target, mode=mode) as archive:
        for name, body in sorted(entries.items()):
            item = tarfile.TarInfo(name)
            item.size = len(body)
            item.mtime = 0
            item.mode = 0o644
            archive.addfile(item, io.BytesIO(body))
    return target.getvalue()


def _zip(entries: dict[str, bytes]) -> bytes:
    target = io.BytesIO()
    with zipfile.ZipFile(target, "w", zipfile.ZIP_DEFLATED) as archive:
        for name, body in sorted(entries.items()):
            info = zipfile.ZipInfo(name, (2020, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o644 << 16
            archive.writestr(info, body)
    return target.getvalue()


def oci_artifact(path: str, wave: str, payload_bytes: int) -> Artifact | None:
    repo = f"library/concurrency-{wave}"
    if path == "/oci/v2/":
        return Artifact(b"{}", "application/json", {})
    manifest, config, layer = _oci_documents(wave, payload_bytes)
    prefix = f"/oci/v2/{repo}/"
    if path == prefix + "manifests/latest":
        return manifest
    if path == prefix + "blobs/" + config.headers["Docker-Content-Digest"]:
        return config
    if path == prefix + "blobs/" + layer.headers["Docker-Content-Digest"]:
        return layer
    return None


@lru_cache(maxsize=8)
def _oci_documents(wave: str, payload_bytes: int) -> tuple[Artifact, Artifact, Artifact]:
    source = f"package wave {wave}\n".encode() + _payload(wave, payload_bytes)
    layer = gzip.compress(tar_bytes({"payload.txt": source}), mtime=0)
    config = json.dumps({
        "architecture": "amd64", "os": "linux", "rootfs": {
            "type": "layers", "diff_ids": ["sha256:" + sha256(gzip.decompress(layer))],
        },
    }, separators=(",", ":"), sort_keys=True).encode()
    layer_digest = "sha256:" + sha256(layer)
    config_digest = "sha256:" + sha256(config)
    manifest = json.dumps({
        "schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
        "config": {"mediaType": "application/vnd.oci.image.config.v1+json", "size": len(config), "digest": config_digest},
        "layers": [{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "size": len(layer), "digest": layer_digest}],
    }, separators=(",", ":"), sort_keys=True).encode()
    return (
        Artifact(manifest, "application/vnd.oci.image.manifest.v1+json", {
            "Docker-Content-Digest": "sha256:" + sha256(manifest),
            "Docker-Distribution-Api-Version": "registry/2.0",
        }),
        Artifact(config, "application/vnd.oci.image.config.v1+json", {"Docker-Content-Digest": config_digest}),
        Artifact(layer, "application/vnd.oci.image.layer.v1.tar+gzip", {"Docker-Content-Digest": layer_digest}),
    )


@lru_cache(maxsize=32)
def artifact_for(path: str, wave: str, payload_bytes: int = 131072) -> Artifact | None:
    """Return a deterministic artifact for one known local route."""
    oci = oci_artifact(path, wave, payload_bytes)
    if oci is not None:
        return oci
    module = f"example.test/demo-{wave}"
    module_root = f"{module}@v{VERSION}/"
    if path == f"/go/{module}/@v/v{VERSION}.info":
        return Artifact(b'{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}', "application/json", {})
    if path == f"/go/{module}/@v/v{VERSION}.mod":
        return Artifact(f"module {module}\n\ngo 1.20\n".encode(), "text/plain", {})
    if path == f"/go/{module}/@v/v{VERSION}.zip":
        body = _zip({module_root + "go.mod": f"module {module}\n\ngo 1.20\n".encode(), module_root + "demo.go": b"package demo\n\nconst Version = \"1.0.0\"\n", module_root + "payload.bin": _payload(wave, payload_bytes)})
        return Artifact(body, "application/zip", {})

    npm_name = f"concurrency-fixture-{wave}"
    npm_tgz = _gzip_tar({
        "package/package.json": _package_json(npm_name, wave),
        "package/index.js": f"module.exports = {{ version: '{VERSION}', wave: '{wave}' }};\n".encode(),
        "package/payload.bin": _payload(wave, payload_bytes),
    })
    if path == f"/npm/{npm_name}":
        meta = {"name": npm_name, "dist-tags": {"latest": VERSION}, "versions": {
            VERSION: {"name": npm_name, "version": VERSION, "dist": {"tarball": f"https://fixture.invalid/npm/{npm_name}/-/{npm_name}-{VERSION}.tgz"}},
        }}
        return Artifact(json.dumps(meta, separators=(",", ":"), sort_keys=True).encode(), "application/vnd.npm.install-v1+json", {"X-Fixture-Rewrite": "tarball-url"})
    if path == f"/npm/{npm_name}/-/{npm_name}-{VERSION}.tgz":
        return Artifact(npm_tgz, "application/gzip", {})

    pypi_name = f"concurrency-fixture-{wave}"
    wheel_name = f"{pypi_name.replace('-', '_')}-{VERSION}-py3-none-any.whl"
    wheel = _wheel(pypi_name.replace("-", "_"), wave, _payload(wave, payload_bytes))
    digest = sha256(wheel)
    if path == f"/pypi/simple/{pypi_name}/":
        html = f'<html><body><a href="/pypi/files/{wheel_name}#sha256={digest}">{wheel_name}</a></body></html>'
        return Artifact(html.encode(), "text/html; charset=utf-8", {})
    if path == f"/pypi/files/{wheel_name}":
        return Artifact(wheel, "application/octet-stream", {})

    group_path = f"example/test/demo-{wave}/{VERSION}/demo-{wave}-{VERSION}"
    if path == f"/maven/{group_path}.pom":
        pom = f"""<project xmlns="http://maven.apache.org/POM/4.0.0" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 https://maven.apache.org/xsd/maven-4.0.0.xsd"><modelVersion>4.0.0</modelVersion><groupId>example.test</groupId><artifactId>demo-{wave}</artifactId><version>{VERSION}</version><packaging>jar</packaging></project>"""
        return Artifact(pom.encode(), "application/xml", {})
    if path == f"/maven/{group_path}.jar":
        return Artifact(_zip({"META-INF/MANIFEST.MF": b"Manifest-Version: 1.0\n\n", "fixture.txt": wave.encode(), "fixture-payload.bin": _payload(wave, payload_bytes)}), "application/java-archive", {})

    dist_name = f"node-v{VERSION}-{wave}-linux-x64.tar.xz"
    if path == f"/dist/v{VERSION}/{dist_name}":
        tar = tar_bytes({f"node-v{VERSION}-linux-x64/bin/node": b"synthetic executable fixture\n", f"node-v{VERSION}-linux-x64/share/fixture/payload.bin": _payload(wave, payload_bytes)})
        return Artifact(lzma.compress(tar, format=lzma.FORMAT_XZ), "application/x-xz", {})
    return None


def _package_json(name: str, wave: str) -> bytes:
    return json.dumps({"name": name, "version": VERSION, "description": f"offline fixture {wave}", "main": "index.js"}, sort_keys=True).encode()


def _gzip_tar(entries: dict[str, bytes]) -> bytes:
    return gzip.compress(tar_bytes(entries), mtime=0)


@lru_cache(maxsize=8)
def _payload(wave: str, size: int) -> bytes:
    return hashlib.shake_256(wave.encode()).digest(size)


def _wheel(name: str, wave: str, payload: bytes) -> bytes:
    dist_info = f"{name}-{VERSION}.dist-info"
    files = {
        f"{name}/__init__.py": f'VERSION = "{VERSION}"\nWAVE = "{wave}"\n'.encode(),
        f"{name}/data.bin": payload,
        f"{dist_info}/METADATA": f"Metadata-Version: 2.1\nName: {name}\nVersion: {VERSION}\n\n".encode(),
        f"{dist_info}/WHEEL": b"Wheel-Version: 1.0\nGenerator: regimux-fixture\nRoot-Is-Purelib: true\nTag: py3-none-any\n",
    }
    record = io.StringIO(newline="")
    writer = csv.writer(record, lineterminator="\n")
    for path, body in sorted(files.items()):
        encoded = base64.urlsafe_b64encode(hashlib.sha256(body).digest()).rstrip(b"=").decode("ascii")
        writer.writerow((path, f"sha256={encoded}", str(len(body))))
    writer.writerow((f"{dist_info}/RECORD", "", ""))
    files[f"{dist_info}/RECORD"] = record.getvalue().encode()
    return _zip(files)
