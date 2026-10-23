"""Isolated PyPI source with one real idle interruption and exact byte ranges."""
from __future__ import annotations

import base64
import hashlib
import io
import json
import os
import re
import select
import socket
import threading
import zipfile
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import BinaryIO

WHEEL_NAME = "deepseek_harness_runtime_bin-0.1.5rc1-py3-none-macosx_14_0_arm64.whl"
WHEEL_SIZE = 75_545_685
WHEEL_SHA256 = "65f20c541a499f08c36ac3bfdafce350ca8c7cc23488c1faf4d9b9524b547156"
DEMO_NAME = "regimux_demo-1.0.0-py3-none-any.whl"
PREFIX_SIZE = 8 << 20
CHUNK_SIZE = 64 << 10
ETAG = '"regimux-pypi-original-wheel-v1"'
MODIFIED = "Wed, 21 Oct 2026 00:00:00 GMT"


def file_digest(path: Path, limit: int | None = None) -> str:
    """Hash a bounded stream without retaining wheel bytes."""
    digest = hashlib.sha256()
    remaining = path.stat().st_size if limit is None else limit
    with path.open("rb") as source:
        while remaining:
            chunk = source.read(min(CHUNK_SIZE, remaining))
            if not chunk:
                raise ValueError("wheel ended before its declared size")
            digest.update(chunk)
            remaining -= len(chunk)
    return digest.hexdigest()


def demo_wheel() -> bytes:
    entries = {
        "regimux_demo/__init__.py": b'__version__ = "1.0.0"\n',
        "regimux_demo/padding.bin": bytes(range(256)) * 1024,
        "regimux_demo-1.0.0.dist-info/METADATA": (
            b"Metadata-Version: 2.1\nName: regimux-demo\nVersion: 1.0.0\nRequires-Python: >=3.8\n\n"
        ),
        "regimux_demo-1.0.0.dist-info/WHEEL": (
            b"Wheel-Version: 1.0\nGenerator: regimux-acceptance\nRoot-Is-Purelib: true\nTag: py3-none-any\n\n"
        ),
    }
    records: list[str] = []
    for name, data in entries.items():
        encoded = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=").decode()
        records.append(f"{name},sha256={encoded},{len(data)}\n")
    record_name = "regimux_demo-1.0.0.dist-info/RECORD"
    entries[record_name] = ("".join(records) + f"{record_name},,\n").encode()
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_STORED) as archive:
        for name, data in entries.items():
            archive.writestr(name, data)
    return output.getvalue()


@dataclass
class State:
    wheel: Path
    demo: bytes
    prefix_sha256: str
    idle_claimed: bool = False
    idle_started: bool = False
    idle_canceled: bool = False
    requests: list[dict[str, str | int | bool]] = field(default_factory=lambda: list[dict[str, str | int | bool]]())
    lock: threading.Lock = field(default_factory=threading.Lock)

    def snapshot(self) -> dict[str, object]:
        with self.lock:
            return {
                "filename": WHEEL_NAME, "size": WHEEL_SIZE, "sha256": WHEEL_SHA256,
                "prefix_sha256": self.prefix_sha256, "idle_started": self.idle_started,
                "idle_canceled": self.idle_canceled, "requests": [dict(item) for item in self.requests],
            }


class FixtureServer(ThreadingHTTPServer):
    def __init__(self, state: State) -> None:
        super().__init__(("0.0.0.0", 8081), Handler)
        self.state = state


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    @property
    def state(self) -> State:
        if not isinstance(self.server, FixtureServer):
            raise TypeError("unexpected fixture server")
        return self.server.state

    def log_message(self, format: str, *args: object) -> None:
        print(format % args, flush=True)

    def do_HEAD(self) -> None:
        self.serve()

    def do_GET(self) -> None:
        self.serve()

    def serve(self) -> None:
        path = self.path.split("?", 1)[0]
        if path in {"/healthz", "/__state"}:
            self.send_bytes(json.dumps(self.state.snapshot()).encode(), "application/json")
            return
        project = path.removeprefix("/simple/").strip("/")
        if path.startswith("/simple/") and project in {"regimux-demo", "deepseek-harness-runtime-bin"}:
            name = DEMO_NAME if project == "regimux-demo" else WHEEL_NAME
            digest = hashlib.sha256(self.state.demo).hexdigest() if name == DEMO_NAME else WHEEL_SHA256
            link = f'<html><a href="http://fixture:8081/packages/{name}#sha256={digest}">{name}</a></html>'
            self.send_bytes(link.encode(), "text/html; charset=utf-8")
            return
        if path == "/packages/" + WHEEL_NAME:
            with self.state.wheel.open("rb") as source:
                self.send_artifact(source, WHEEL_SIZE, True)
            return
        if path == "/packages/" + DEMO_NAME:
            self.send_artifact(io.BytesIO(self.state.demo), len(self.state.demo), False)
            return
        self.send_error(404)

    def send_bytes(self, data: bytes, content_type: str) -> None:
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(data)

    def send_artifact(self, source: BinaryIO, size: int, original: bool) -> None:
        selected = self.byte_range(size)
        if selected is None:
            return
        start, end, status = selected
        self.send_response(status)
        for name, value in {
            "Content-Length": str(end - start + 1), "Content-Type": "application/octet-stream",
            "Accept-Ranges": "bytes", "ETag": ETAG, "Last-Modified": MODIFIED,
        }.items():
            self.send_header(name, value)
        if status == 206:
            self.send_header("Content-Range", f"bytes {start}-{end}/{size}")
        self.end_headers()
        record: dict[str, str | int | bool] = {
            "path": self.path, "range": self.headers.get("Range", ""),
            "if_range": self.headers.get("If-Range", ""), "status": status,
            "start": start, "end": end, "sent": 0, "original": original,
        }
        with self.state.lock:
            self.state.requests.append(record)
            pause = original and self.command == "GET" and status == 200 and not self.state.idle_claimed
            if pause:
                self.state.idle_claimed = True
        if self.command == "HEAD":
            return
        source.seek(start)
        try:
            if pause:
                self.copy_bytes(source, PREFIX_SIZE, record)
                self.wfile.flush()
                with self.state.lock:
                    self.state.idle_started = True
                ready, _, _ = select.select([self.connection], [], [], 31)
                if ready and not self.connection.recv(1, socket.MSG_PEEK):
                    with self.state.lock:
                        self.state.idle_canceled = True
                    return
            self.copy_bytes(source, end - start + 1 - int(record["sent"]), record)
        except (BrokenPipeError, ConnectionResetError):
            pass
        finally:
            print(json.dumps(record), flush=True)

    def copy_bytes(self, source: BinaryIO, size: int, record: dict[str, str | int | bool]) -> None:
        while size:
            chunk = source.read(min(CHUNK_SIZE, size))
            if not chunk:
                raise ValueError("fixture source unexpectedly ended")
            self.wfile.write(chunk)
            with self.state.lock:
                record["sent"] = int(record["sent"]) + len(chunk)
            size -= len(chunk)

    def byte_range(self, size: int) -> tuple[int, int, int] | None:
        value = self.headers.get("Range", "")
        if not value or self.headers.get("If-Range", ETAG) not in {ETAG, MODIFIED}:
            return 0, size - 1, 200
        match = re.fullmatch(r"bytes=(\d*)-(\d*)", value)
        if match:
            left, right = match.groups()
            if left:
                start, end = int(left), min(int(right), size - 1) if right else size - 1
            else:
                start, end = max(0, size - int(right or "0")), size - 1
            if 0 <= start <= end < size:
                return start, end, 206
        self.send_response(416)
        self.send_header("Content-Range", f"bytes */{size}")
        self.send_header("Content-Length", "0")
        self.end_headers()
        return None


def main() -> None:
    wheel = Path(os.environ.get("PYPI_WHEEL_PATH", "/wheel/public.whl"))
    if wheel.stat().st_size != WHEEL_SIZE or file_digest(wheel) != WHEEL_SHA256:
        raise ValueError("public wheel does not match the issue's pinned size and SHA-256")
    state = State(wheel, demo_wheel(), file_digest(wheel, PREFIX_SIZE))
    print(json.dumps({"event": "source_verified", "size": WHEEL_SIZE, "sha256": WHEEL_SHA256}), flush=True)
    FixtureServer(state).serve_forever()


if __name__ == "__main__":
    main()
