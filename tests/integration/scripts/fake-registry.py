#!/usr/bin/env python3
import hashlib
import json
import os
import threading
import time
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse


REPO = os.environ.get("REGIMUX_FAKE_REGISTRY_REPO", "library/regimux-fixture").strip("/")
TAG = os.environ.get("REGIMUX_FAKE_REGISTRY_TAG", "latest")
HOST = os.environ.get("REGIMUX_FAKE_REGISTRY_HOST", "0.0.0.0")
PORT = int(os.environ.get("REGIMUX_FAKE_REGISTRY_PORT", "5000"))
CHUNK_SIZE = int(os.environ.get("REGIMUX_FAKE_REGISTRY_CHUNK_SIZE", "16384"))
CHUNK_DELAY = float(os.environ.get("REGIMUX_FAKE_REGISTRY_CHUNK_DELAY_SECONDS", "0.01"))


def digest(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


LAYER_BODY = (b"regimux multi replica integration blob\n" + bytes(range(256))) * 4096
CONFIG_BODY = b'{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}'
LAYER_DIGEST = digest(LAYER_BODY)
CONFIG_DIGEST = digest(CONFIG_BODY)
MANIFEST_BODY = json.dumps(
    {
        "schemaVersion": 2,
        "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
        "config": {
            "mediaType": "application/vnd.docker.container.image.v1+json",
            "size": len(CONFIG_BODY),
            "digest": CONFIG_DIGEST,
        },
        "layers": [
            {
                "mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
                "size": len(LAYER_BODY),
                "digest": LAYER_DIGEST,
            }
        ],
    },
    separators=(",", ":"),
    sort_keys=True,
).encode("utf-8")
MANIFEST_DIGEST = digest(MANIFEST_BODY)


class RequestCounts:
    def __init__(self):
        self._lock = threading.Lock()
        self._counts = {
            "manifest_gets": 0,
            "manifest_heads": 0,
            "blob_gets": 0,
            "blob_heads": 0,
            "layer_blob_gets": 0,
            "layer_blob_heads": 0,
        }

    def increment(self, key):
        with self._lock:
            self._counts[key] += 1
            return dict(self._counts)

    def snapshot(self):
        with self._lock:
            return dict(self._counts)


COUNTS = RequestCounts()


class RegistryHandler(BaseHTTPRequestHandler):
    server_version = "RegiMuxFakeRegistry/1.0"

    def do_GET(self):
        self.handle_request(send_body=True)

    def do_HEAD(self):
        self.handle_request(send_body=False)

    def handle_request(self, send_body):
        path = urlparse(self.path).path
        if path == "/healthz":
            self.write_bytes(HTTPStatus.OK, b"ok\n", "text/plain", send_body)
            return
        if path == "/v2/" or path == "/v2":
            self.write_bytes(HTTPStatus.OK, b"", "application/json", send_body)
            return
        if path == "/__debug__/fixture":
            self.write_json(
                {
                    "repo": REPO,
                    "tag": TAG,
                    "manifest_digest": MANIFEST_DIGEST,
                    "layer_digest": LAYER_DIGEST,
                    "layer_size": len(LAYER_BODY),
                    "config_digest": CONFIG_DIGEST,
                    "config_size": len(CONFIG_BODY),
                },
                send_body,
            )
            return
        if path == "/__debug__/requests":
            self.write_json(COUNTS.snapshot(), send_body)
            return

        manifest_prefix = f"/v2/{REPO}/manifests/"
        blob_prefix = f"/v2/{REPO}/blobs/"
        if path.startswith(manifest_prefix):
            reference = path[len(manifest_prefix) :]
            if reference in (TAG, MANIFEST_DIGEST):
                self.handle_manifest(send_body)
                return
        if path.startswith(blob_prefix):
            requested = path[len(blob_prefix) :]
            if requested == LAYER_DIGEST:
                self.handle_blob(LAYER_BODY, LAYER_DIGEST, "application/octet-stream", send_body, layer=True)
                return
            if requested == CONFIG_DIGEST:
                self.handle_blob(
                    CONFIG_BODY,
                    CONFIG_DIGEST,
                    "application/vnd.docker.container.image.v1+json",
                    send_body,
                    layer=False,
                )
                return

        self.write_json({"errors": [{"code": "NAME_UNKNOWN", "message": "fixture path not found"}]}, send_body, HTTPStatus.NOT_FOUND)

    def handle_manifest(self, send_body):
        if self.command == "HEAD":
            COUNTS.increment("manifest_heads")
        else:
            COUNTS.increment("manifest_gets")
        self.send_response(HTTPStatus.OK)
        self.send_header("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
        self.send_header("Content-Length", str(len(MANIFEST_BODY)))
        self.send_header("Docker-Content-Digest", MANIFEST_DIGEST)
        self.send_header("Docker-Distribution-Api-Version", "registry/2.0")
        self.end_headers()
        if send_body:
            self.wfile.write(MANIFEST_BODY)

    def handle_blob(self, body, body_digest, content_type, send_body, layer):
        if self.command == "HEAD":
            COUNTS.increment("blob_heads")
            if layer:
                COUNTS.increment("layer_blob_heads")
        else:
            COUNTS.increment("blob_gets")
            if layer:
                COUNTS.increment("layer_blob_gets")
        self.send_response(HTTPStatus.OK)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Docker-Content-Digest", body_digest)
        self.send_header("Docker-Distribution-Api-Version", "registry/2.0")
        self.end_headers()
        if not send_body:
            return
        if layer:
            self.write_slow(body)
            return
        self.wfile.write(body)

    def write_slow(self, body):
        for offset in range(0, len(body), CHUNK_SIZE):
            chunk = body[offset : offset + CHUNK_SIZE]
            try:
                self.wfile.write(chunk)
                self.wfile.flush()
            except BrokenPipeError:
                return
            if CHUNK_DELAY > 0:
                time.sleep(CHUNK_DELAY)

    def write_json(self, payload, send_body, status=HTTPStatus.OK):
        body = json.dumps(payload, separators=(",", ":"), sort_keys=True).encode("utf-8")
        self.write_bytes(status, body, "application/json", send_body)

    def write_bytes(self, status, body, content_type, send_body):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if send_body:
            self.wfile.write(body)

    def log_message(self, fmt, *args):
        return


def main():
    server = ThreadingHTTPServer((HOST, PORT), RegistryHandler)
    print(f"fake registry listening on {HOST}:{PORT} repo={REPO} layer={LAYER_DIGEST}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
