"""Shared module loader and wire backend for S3 proxy regression tests."""
from __future__ import annotations

import importlib.util
import os
import socketserver
from pathlib import Path
from types import ModuleType
from typing import cast


def load_s3_proxy() -> ModuleType:
    selected = os.environ.get("REGIMUX_TEST_S3_PROXY_MODULE")
    if not selected:
        import s3_proxy

        return s3_proxy
    module_path = Path(selected).expanduser()
    if not module_path.is_absolute():
        module_path = Path.cwd() / module_path
    module_path = module_path.resolve()
    spec = importlib.util.spec_from_file_location("s3_proxy", module_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load S3 proxy module from {module_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


s3_proxy = load_s3_proxy()


class BackendServer(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, response: bytes) -> None:
        self.response = response
        self.received = b""
        super().__init__(("127.0.0.1", 0), BackendHandler)


class BackendHandler(socketserver.BaseRequestHandler):
    def handle(self) -> None:
        backend = cast(BackendServer, self.server)
        reader = self.request.makefile("rb")
        request = bytearray()
        while line := reader.readline(16385):
            request.extend(line)
            if line == b"\r\n":
                break
        headers = bytes(request).lower()
        if b"transfer-encoding: chunked\r\n" in headers:
            while line := reader.readline(16385):
                request.extend(line)
                size = int(line.split(b";", 1)[0], 16)
                if size == 0:
                    while trailer := reader.readline(16385):
                        request.extend(trailer)
                        if trailer == b"\r\n":
                            break
                    break
                request.extend(reader.read(size + 2))
        elif b"content-length:" in headers:
            length = next(
                int(line.split(b":", 1)[1])
                for line in headers.splitlines()
                if line.startswith(b"content-length:")
            )
            request.extend(reader.read(length))
        backend.received = bytes(request)
        self.request.sendall(backend.response)
