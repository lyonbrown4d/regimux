"""Threaded offline upstream fixture for high-concurrency cache acceptance."""
from __future__ import annotations

import hashlib
import json
import os
import re
import threading
import time
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import TypedDict
from urllib.parse import parse_qs, urlsplit

from assets import Artifact, VERSION, artifact_for


class FixtureRecord(TypedDict, total=False):
    name: str
    ecosystem: str
    path: str
    upstream_path: str
    status: int
    sha256: str
    markers: list[str]
    body_bytes: int


HOST = os.environ.get("FIXTURE_HOST", "0.0.0.0")
PORT = int(os.environ.get("FIXTURE_PORT", "8081"))
DEFAULT_DELAY_MS = max(0, min(10000, int(os.environ.get("FIXTURE_DELAY_MS", "100"))))
PAYLOAD_BYTES = max(0, min(1048576, int(os.environ.get("FIXTURE_PAYLOAD_BYTES", "131072"))))
COUNTS: dict[str, dict[str, int]] = {}
COUNTS_LOCK = threading.Lock()
RESET_LOCK = threading.Lock()
WAVE_RE = re.compile(r"^[a-z0-9][a-z0-9-]{0,47}$")


def fixture_records(wave: str) -> list[FixtureRecord]:
    """Return stable metadata for every proxyable cold object in a wave."""
    module = f"example.test/demo-{wave}"
    npm = f"concurrency-fixture-{wave}"
    wheel = f"concurrency_fixture_{wave.replace('-', '_')}-{VERSION}-py3-none-any.whl"
    wheel_artifact = _artifact_from_record("pypi-wheel", wave)
    wheel_sha256 = hashlib.sha256(wheel_artifact.body).hexdigest() if wheel_artifact else ""
    maven = f"example/test/demo-{wave}/{VERSION}/demo-{wave}-{VERSION}"
    dist = f"node-v{VERSION}-{wave}-linux-x64.tar.xz"
    paths = [
        ("oci-manifest", "oci", f"/oci/v2/library/concurrency-{wave}/manifests/latest", f"/v2/hub/library/concurrency-{wave}/manifests/latest"),
        ("oci-config", "oci", _oci_blob_path(wave, "config"), f"/v2/hub/library/concurrency-{wave}/blobs/{_oci_digest(wave, 'config')}"),
        ("oci-layer", "oci", _oci_blob_path(wave, "layer"), f"/v2/hub/library/concurrency-{wave}/blobs/{_oci_digest(wave, 'layer')}"),
        ("go-info", "go", f"/go/{module}/@v/v{VERSION}.info", f"/go/default/{module}/@v/v{VERSION}.info"),
        ("go-mod", "go", f"/go/{module}/@v/v{VERSION}.mod", f"/go/default/{module}/@v/v{VERSION}.mod"),
        ("go-zip", "go", f"/go/{module}/@v/v{VERSION}.zip", f"/go/default/{module}/@v/v{VERSION}.zip"),
        ("npm-metadata", "npm", f"/npm/{npm}", f"/npm/default/{npm}"),
        ("npm-tarball", "npm", f"/npm/{npm}/-/{npm}-{VERSION}.tgz", f"/npm/default/{npm}/-/{npm}-{VERSION}.tgz"),
        ("pypi-simple", "pypi", f"/pypi/simple/{npm}/", f"/pypi/default/simple/{npm}/"),
        ("pypi-wheel", "pypi", f"/pypi/files/{wheel}", f"/pypi/default/packages/http/fixture:8081/pypi/files/{wheel}?__regimux_project={npm}&__regimux_sha256={wheel_sha256}"),
        ("maven-pom", "maven", f"/maven/{maven}.pom", f"/maven/central/{maven}.pom"),
        ("maven-jar", "maven", f"/maven/{maven}.jar", f"/maven/central/{maven}.jar"),
        ("dist-archive", "dist", f"/dist/v{VERSION}/{dist}", f"/dist/nodejs/v{VERSION}/{dist}"),
    ]
    records: list[FixtureRecord] = []
    for label, ecosystem, upstream_path, proxy_path in paths:
        artifact = _artifact_from_record(label, wave)
        markers = {
            "npm-metadata": [f"/npm/default/{npm}/-/{npm}-{VERSION}.tgz"],
            "pypi-simple": [f"/pypi/default/packages/http/fixture:8081/pypi/files/{wheel}"],
        }.get(label, [])
        entry = FixtureRecord(name=label, ecosystem=ecosystem, path=proxy_path,
                              upstream_path=upstream_path, status=int(HTTPStatus.OK))
        if markers:
            entry["markers"] = markers
        if artifact is not None:
            served_body = artifact.body
            if artifact.headers.get("X-Fixture-Rewrite") == "tarball-url":
                served_body = served_body.replace(b"https://fixture.invalid", b"http://fixture:8081")
            entry["body_bytes"] = len(served_body)
            if not markers:
                entry["sha256"] = hashlib.sha256(artifact.body).hexdigest()
        records.append(entry)
    return records


def _artifact_from_record(label: str, wave: str) -> Artifact | None:
    # The records table intentionally contains route templates; use exact canonical URLs here.
    module = f"example.test/demo-{wave}"
    npm = f"concurrency-fixture-{wave}"
    wheel = f"concurrency_fixture_{wave.replace('-', '_')}-{VERSION}-py3-none-any.whl"
    maven = f"example/test/demo-{wave}/{VERSION}/demo-{wave}-{VERSION}"
    dist = f"node-v{VERSION}-{wave}-linux-x64.tar.xz"
    route = {
        "oci-manifest": f"/oci/v2/library/concurrency-{wave}/manifests/latest",
        "go-info": f"/go/{module}/@v/v{VERSION}.info",
        "go-mod": f"/go/{module}/@v/v{VERSION}.mod",
        "go-zip": f"/go/{module}/@v/v{VERSION}.zip",
        "npm-metadata": f"/npm/{npm}",
        "npm-tarball": f"/npm/{npm}/-/{npm}-{VERSION}.tgz",
        "pypi-simple": f"/pypi/simple/{npm}/",
        "pypi-wheel": f"/pypi/files/{wheel}",
        "maven-pom": f"/maven/{maven}.pom",
        "maven-jar": f"/maven/{maven}.jar",
        "dist-archive": f"/dist/v{VERSION}/{dist}",
    }
    if label in ("oci-config", "oci-layer"):
        return artifact_for(_oci_blob_path(wave, "config" if label == "oci-config" else "layer"), wave, PAYLOAD_BYTES)
    return artifact_for(route[label], wave, PAYLOAD_BYTES) if label in route else None


def _oci_blob_path(wave: str, kind: str) -> str:
    manifest_path = f"/oci/v2/library/concurrency-{wave}/manifests/latest"
    manifest = artifact_for(manifest_path, wave, PAYLOAD_BYTES)
    if manifest is None:
        return ""
    value = json.loads(manifest.body)
    digest = value["config"]["digest"] if kind == "config" else value["layers"][0]["digest"]
    return f"/oci/v2/library/concurrency-{wave}/blobs/{digest}"


def _oci_digest(wave: str, kind: str) -> str:
    return _oci_blob_path(wave, kind).rsplit("/", 1)[-1]


class FixtureHandler(BaseHTTPRequestHandler):
    server_version = "RegiMuxOfflineFixture/1.0"

    def do_GET(self) -> None:
        self._serve(send_body=True)

    def do_HEAD(self) -> None:
        self._serve(send_body=False)

    def do_POST(self) -> None:
        if urlsplit(self.path).path != "/__reset":
            self._write(HTTPStatus.NOT_FOUND, b"not found\n", "text/plain")
            return
        query = parse_qs(urlsplit(self.path).query)
        prefix = query.get("prefix", [""])[0]
        with RESET_LOCK, COUNTS_LOCK:
            if prefix:
                for key in tuple(COUNTS):
                    if key.startswith(prefix):
                        del COUNTS[key]
            else:
                COUNTS.clear()
        self._write(HTTPStatus.OK, b'{"reset":true}\n', "application/json")

    def _serve(self, send_body: bool) -> None:
        parsed = urlsplit(self.path)
        if parsed.path in ("/__fixtures", "/__fixtures/"):
            wave = self._wave(parse_qs(parsed.query))
            if wave is None:
                self._json(HTTPStatus.BAD_REQUEST, {"error": "wave must match [a-z0-9][a-z0-9-]{0,47}"}, send_body)
                return
            self._json(HTTPStatus.OK, {"wave": wave, "delay_ms": self._delay(parse_qs(parsed.query)), "targets": fixture_records(wave)}, send_body)
            return
        if parsed.path == "/__stats":
            with COUNTS_LOCK:
                snapshot = {path: dict(sorted(methods.items())) for path, methods in sorted(COUNTS.items())}
            self._json(HTTPStatus.OK, {"requests": snapshot}, send_body)
            return
        if parsed.path in ("/healthz", "/health"):
            self._write(HTTPStatus.OK, b"ok\n", "text/plain", send_body)
            return
        for prefix, status in (("/maven-missing", HTTPStatus.NOT_FOUND),
                               ("/maven-unavailable", HTTPStatus.SERVICE_UNAVAILABLE),
                               ("/maven-denied", HTTPStatus.UNAUTHORIZED)):
            if parsed.path.startswith(prefix + "/"):
                self._count(self.command, parsed.path)
                self._write(status, f"synthetic Maven {status.value}\n".encode(), "text/plain", send_body)
                return
        if parsed.path == "/oci/v2/":
            artifact = artifact_for(parsed.path, "", PAYLOAD_BYTES)
            if artifact is not None:
                self._count(self.command, parsed.path)
                self._write(HTTPStatus.OK, artifact.body, artifact.content_type, send_body, artifact.headers)
                return
        wave = self._wave(parse_qs(parsed.query))
        if wave is None:
            wave = self._wave_from_path(parsed.path)
        if wave is None:
            self._json(HTTPStatus.NOT_FOUND, {"error": "unknown local fixture path"}, send_body)
            return
        artifact = artifact_for(parsed.path, wave, PAYLOAD_BYTES)
        if artifact is None:
            self._json(HTTPStatus.NOT_FOUND, {"error": "fixture object not found", "path": parsed.path}, send_body)
            return
        self._count(self.command, parsed.path)
        if send_body and self._delay(parse_qs(parsed.query)):
            time.sleep(self._delay(parse_qs(parsed.query)) / 1000)
        body = artifact.body
        if artifact.headers.get("X-Fixture-Rewrite") == "tarball-url":
            body = body.replace(b"https://fixture.invalid", b"http://fixture:8081")
        self._write(HTTPStatus.OK, body, artifact.content_type, send_body, artifact.headers)

    @staticmethod
    def _wave(query: dict[str, list[str]]) -> str | None:
        value = query.get("wave", [""])[0]
        return value if WAVE_RE.fullmatch(value) else None

    @staticmethod
    def _wave_from_path(path: str) -> str | None:
        patterns = (
            r"/library/concurrency-([a-z0-9][a-z0-9-]{0,47})/",
            r"/demo-([a-z0-9][a-z0-9-]{0,47})/",
            r"/concurrency-fixture-([a-z0-9][a-z0-9-]{0,47})(?:/|$)",
            r"/concurrency_fixture_([a-z0-9_-]+)-1\.0\.0-py3-none-any\.whl$",
            r"/node-v1\.0\.0-([a-z0-9][a-z0-9-]{0,47})-linux-x64",
        )
        for pattern in patterns:
            match = re.search(pattern, path)
            if match:
                return match.group(1).replace("_", "-").rstrip("-/.")
        return None

    @staticmethod
    def _delay(query: dict[str, list[str]]) -> int:
        raw = query.get("delay_ms", [str(DEFAULT_DELAY_MS)])[0]
        try:
            return max(0, min(10000, int(raw)))
        except ValueError:
            return DEFAULT_DELAY_MS

    @staticmethod
    def _count(method: str, path: str) -> None:
        with COUNTS_LOCK:
            methods = COUNTS.setdefault(path, {"GET": 0, "HEAD": 0})
            methods[method] = methods.get(method, 0) + 1

    def _json(self, status: HTTPStatus, value: object, send_body: bool = True) -> None:
        body = json.dumps(value, separators=(",", ":"), sort_keys=True).encode()
        self._write(status, body, "application/json", send_body)

    def _write(self, status: HTTPStatus, body: bytes, content_type: str, send_body: bool = True, headers: dict[str, str] | None = None) -> None:
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        for name, value in (headers or {}).items():
            if name.lower() != "x-fixture-rewrite":
                self.send_header(name, value)
        self.end_headers()
        if send_body:
            try:
                self.wfile.write(body)
            except (BrokenPipeError, ConnectionResetError):
                pass

    def log_message(self, fmt: str, *args: object) -> None:
        return


class FixtureHTTPServer(ThreadingHTTPServer):
    allow_reuse_address = True
    daemon_threads = True
    request_queue_size = 2048


def main() -> None:
    server = FixtureHTTPServer((HOST, PORT), FixtureHandler)
    print(f"offline fixture listening on {HOST}:{PORT}; default delay {DEFAULT_DELAY_MS} ms; payload {PAYLOAD_BYTES} bytes", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
