"""Bounded streaming S3 HTTP proxy for container memory acceptance."""
from __future__ import annotations

import io
import os
import re
import selectors
import socket
import threading
import time
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import BinaryIO, Callable, cast


HOST = os.environ.get("S3_PROXY_HOST", "0.0.0.0")
PORT = int(os.environ.get("S3_PROXY_PORT", "9000"))
TARGET_HOST = os.environ.get("S3_PROXY_TARGET_HOST", "s3")
TARGET_PORT = int(os.environ.get("S3_PROXY_TARGET_PORT", "9000"))
PUT_DELAY_MS = max(0, min(2000, int(os.environ.get("S3_PROXY_PUT_DELAY_MS", "100"))))
COPY_BYTES = 32 * 1024
MAX_LINE_BYTES = 16 * 1024
MAX_HEADER_BYTES = 64 * 1024
CHUNK_SIZE_RE = re.compile(rb"^[0-9a-fA-F]+(?:;[^\r\n]*)?$")


class _PrefixReader(io.RawIOBase):
    """Replay admission bytes before reading the existing socket file."""

    def __init__(self, prefix: bytes, source: io.BufferedReader) -> None:
        self._prefix = memoryview(prefix)
        self._source = source

    def readable(self) -> bool:
        return True

    def readinto(self, buffer: bytearray | memoryview) -> int:
        if self._prefix:
            size = min(len(buffer), len(self._prefix))
            buffer[:size] = self._prefix[:size]
            self._prefix = self._prefix[size:]
            return size
        # readinto1 performs at most one underlying read. readinto could wait
        # for a full buffer after a short streamed body on a persistent socket.
        return self._source.readinto1(buffer)

    def close(self) -> None:
        try:
            self._source.close()
        finally:
            super().close()


def _copy_exact(source: BinaryIO, send: Callable[[bytes], None], size: int) -> None:
    remaining = size
    while remaining:
        block = source.read(min(COPY_BYTES, remaining))
        if not block:
            raise ConnectionError("truncated HTTP message body")
        send(block)
        remaining -= len(block)


def _copy_chunked(source: BinaryIO, send: Callable[[bytes], None]) -> None:
    while True:
        line = source.readline(MAX_LINE_BYTES + 1)
        if not line or len(line) > MAX_LINE_BYTES or not line.endswith(b"\r\n"):
            raise ConnectionError("invalid or oversized HTTP chunk header")
        size_token = line[:-2].split(b";", 1)[0]
        if not CHUNK_SIZE_RE.fullmatch(line[:-2]):
            raise ConnectionError("invalid HTTP chunk header")
        size = int(size_token, 16)
        send(line)
        if size == 0:
            trailer_bytes = 0
            while True:
                trailer = source.readline(MAX_LINE_BYTES + 1)
                trailer_bytes += len(trailer)
                if not trailer or len(trailer) > MAX_LINE_BYTES or trailer_bytes > MAX_HEADER_BYTES:
                    raise ConnectionError("invalid or oversized HTTP trailer")
                send(trailer)
                if trailer == b"\r\n":
                    return
        _copy_exact(source, send, size + 2)


def _response_headers(source: BinaryIO) -> tuple[bytes, list[bytes], int, dict[str, str]]:
    status_line = source.readline(MAX_LINE_BYTES + 1)
    if not status_line or len(status_line) > MAX_LINE_BYTES or not status_line.endswith(b"\r\n"):
        raise ConnectionError("invalid S3 response status line")
    header_lines: list[bytes] = []
    parsed: dict[str, str] = {}
    total = len(status_line)
    while True:
        line = source.readline(MAX_LINE_BYTES + 1)
        total += len(line)
        if not line or len(line) > MAX_LINE_BYTES or total > MAX_HEADER_BYTES:
            raise ConnectionError("invalid or oversized S3 response headers")
        if line == b"\r\n":
            break
        header_lines.append(line)
        if b":" in line:
            name, value = line.split(b":", 1)
            parsed[name.decode("latin1").strip().lower()] = value.decode("latin1").strip()
    parts = status_line.split(None, 2)
    if len(parts) < 2:
        raise ConnectionError("invalid S3 response status")
    return status_line, header_lines, int(parts[1]), parsed


class S3ProxyHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    server_version = "RegiMuxMemoryS3Proxy/1.0"

    def setup(self) -> None:
        super().setup()
        server = cast(BoundedProxyServer, self.server)
        prefix = server.take_prefix(self.connection)
        if prefix:
            self.rfile = io.BufferedReader(_PrefixReader(prefix, cast(io.BufferedReader, self.rfile)))

    def do_GET(self) -> None:
        self._forward()

    def do_HEAD(self) -> None:
        self._forward()

    def do_POST(self) -> None:
        self._forward()

    def do_PUT(self) -> None:
        self._forward()

    def do_DELETE(self) -> None:
        self._forward()

    def do_OPTIONS(self) -> None:
        self._forward()

    def handle_expect_100(self) -> bool:
        self.send_response_only(HTTPStatus.CONTINUE)
        self.end_headers()
        return True

    def _forward(self) -> None:
        if self.path == "/__healthz":
            self._health()
            return
        server = cast(BoundedProxyServer, self.server)
        with server._slots:
            self._forward_request()

    def _forward_request(self) -> None:
        self.close_connection = True
        response_started = False
        try:
            with socket.create_connection((TARGET_HOST, TARGET_PORT), timeout=15) as upstream:
                upstream.settimeout(120)
                request_header_parts = [f"{self.command} {self.path} HTTP/1.1\r\n".encode("latin1")]
                transfer_encoding = ""
                content_length: int | None = None
                for name, value in self.headers.raw_items():
                    lowered = name.lower()
                    if lowered in ("connection", "keep-alive", "proxy-connection", "expect"):
                        continue
                    if lowered == "transfer-encoding":
                        transfer_encoding = value.lower()
                    elif lowered == "content-length":
                        content_length = int(value)
                    request_header_parts.append(f"{name}: {value}\r\n".encode("latin1"))
                request_header_parts.append(b"Connection: close\r\n\r\n")
                upstream.sendall(b"".join(request_header_parts))

                if self.command == "PUT" and (content_length or "chunked" in transfer_encoding):
                    if PUT_DELAY_MS:
                        time.sleep(PUT_DELAY_MS / 1000)
                if "chunked" in transfer_encoding:
                    _copy_chunked(self.rfile, upstream.sendall)
                elif content_length:
                    _copy_exact(self.rfile, upstream.sendall, content_length)

                with upstream.makefile("rb") as response:
                    while True:
                        status_line, header_lines, status, headers = _response_headers(response)
                        self._send_response_headers(status_line, header_lines, close=not 100 <= status < 200)
                        response_started = True
                        if status == HTTPStatus.CONTINUE or 100 <= status < 200:
                            continue
                        send_client = self._send_client
                        has_body = self.command != "HEAD" and status not in (
                            HTTPStatus.NO_CONTENT, HTTPStatus.NOT_MODIFIED,
                        )
                        if has_body and "chunked" in headers.get("transfer-encoding", "").lower():
                            _copy_chunked(response, send_client)
                        elif has_body and "content-length" in headers:
                            _copy_exact(response, send_client, int(headers["content-length"]))
                        elif has_body:
                            while block := response.read(COPY_BYTES):
                                send_client(block)
                        break
        except (OSError, ValueError, ConnectionError) as error:
            if response_started:
                self.close_connection = True
            else:
                self.send_error(HTTPStatus.BAD_GATEWAY, "S3 backend unavailable")
            print(f"proxy request failed: {type(error).__name__}", flush=True)

    def _send_response_headers(self, status_line: bytes, header_lines: list[bytes], *, close: bool) -> None:
        response_header_parts = [status_line]
        for line in header_lines:
            name = line.partition(b":")[0].strip().lower()
            if name in (b"connection", b"keep-alive"):
                continue
            response_header_parts.append(line)
        if close:
            response_header_parts.append(b"Connection: close\r\n")
        response_header_parts.append(b"\r\n")
        self.connection.sendall(b"".join(response_header_parts))

    def _send_client(self, data: bytes) -> None:
        self.wfile.write(data)
        self.wfile.flush()

    def _health(self) -> None:
        self.close_connection = True
        try:
            with socket.create_connection((TARGET_HOST, TARGET_PORT), timeout=2):
                status = HTTPStatus.OK
                body = b"ok\n"
        except OSError:
            status = HTTPStatus.SERVICE_UNAVAILABLE
            body = b"unavailable\n"
        self.send_response(status)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def log_message(self, fmt: str, *args: object) -> None:
        return


class _PendingRequest:
    def __init__(self, address: tuple[str, int]) -> None:
        self.address = address
        self.prefix = bytearray()
        self.complete = False


class BoundedProxyServer(ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 128

    def __init__(self, address: tuple[str, int], handler: type[BaseHTTPRequestHandler]) -> None:
        from threading import BoundedSemaphore

        self._slots = BoundedSemaphore(64)
        self._handlers = BoundedSemaphore(64)
        self._health_handlers = BoundedSemaphore(1)
        self._pending: dict[socket.socket, _PendingRequest] = {}
        self._prefixes: dict[socket.socket, bytes] = {}
        self._admissions: dict[socket.socket, threading.BoundedSemaphore] = {}
        self._state_lock = threading.Lock()
        self._pending_selector = selectors.DefaultSelector()
        super().__init__(address, handler)

    def serve_forever(self, poll_interval: float = 0.01) -> None:
        # The base server selects the listener; sample pending socket readiness
        # on this bounded tick as well. No request timeout is changed.
        super().serve_forever(poll_interval)

    def process_request(self, request: socket.socket, client_address: tuple[str, int]) -> None:
        if len(self._pending) >= self.request_queue_size:
            oldest_idle = next((sock for sock, state in self._pending.items() if not state.complete), None)
            if oldest_idle is None:
                self.shutdown_request(request)
                return
            self._drop_pending(oldest_idle)
        request.setblocking(False)
        self._pending[request] = _PendingRequest(client_address)
        self._pending_selector.register(request, selectors.EVENT_READ)

    def service_actions(self) -> None:
        # One bounded recv per ready socket/tick consumes partial headers. They
        # cease being level-trigger ready, rather than spinning on MSG_PEEK.
        for key, _ in self._pending_selector.select(timeout=0):
            request = cast(socket.socket, key.fileobj)
            pending = self._pending[request]
            try:
                block = request.recv(min(COPY_BYTES, MAX_HEADER_BYTES - len(pending.prefix)))
            except BlockingIOError:
                continue
            except OSError:
                self._drop_pending(request)
                continue
            if not block:
                self._drop_pending(request)
                continue
            pending.prefix.extend(block)
            if b"\r\n\r\n" in pending.prefix:
                pending.complete = True
                self._pending_selector.unregister(request)
            elif len(pending.prefix) >= MAX_HEADER_BYTES:
                self._drop_pending(request)

        for request, pending in list(self._pending.items()):
            if not pending.complete:
                continue
            request_line = pending.prefix.split(b"\r\n", 1)[0].split()
            is_health = len(request_line) >= 2 and request_line[1] == b"/__healthz"
            admission = self._health_handlers if is_health else self._handlers
            if not admission.acquire(blocking=False):
                continue
            del self._pending[request]
            request.setblocking(True)
            with self._state_lock:
                self._prefixes[request] = bytes(pending.prefix)
                self._admissions[request] = admission
            try:
                super().process_request(request, pending.address)
            except BaseException:
                self._release_handler(request)
                self.shutdown_request(request)
                raise

    def take_prefix(self, request: socket.socket) -> bytes:
        with self._state_lock:
            return self._prefixes.pop(request)

    def _drop_pending(self, request: socket.socket) -> None:
        if not self._pending.pop(request).complete:
            self._pending_selector.unregister(request)
        self.shutdown_request(request)

    def _release_handler(self, request: socket.socket) -> None:
        with self._state_lock:
            self._prefixes.pop(request, None)
            admission = self._admissions.pop(request)
        admission.release()

    def process_request_thread(self, request: socket.socket, client_address: tuple[str, int]) -> None:
        try:
            super().process_request_thread(request, client_address)
        finally:
            self._release_handler(request)

    def server_close(self) -> None:
        for request in list(self._pending):
            self._drop_pending(request)
        self._pending_selector.close()
        super().server_close()


def main() -> None:
    threading.stack_size(512 * 1024)
    server = BoundedProxyServer((HOST, PORT), S3ProxyHandler)
    print(f"bounded S3 proxy on {HOST}:{PORT} to {TARGET_HOST}:{TARGET_PORT}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
