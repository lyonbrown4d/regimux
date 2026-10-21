"""Protocol-preservation checks for the memory acceptance S3 proxy."""
from __future__ import annotations

import socket
import socketserver
import threading
import unittest
from typing import cast

import s3_proxy


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


class S3ProxyProtocolTests(unittest.TestCase):
    def setUp(self) -> None:
        s3_proxy.PUT_DELAY_MS = 0

    def _proxy_roundtrip(self, request: bytes, response: bytes) -> tuple[bytes, bytes]:
        backend = BackendServer(response)
        proxy_server = s3_proxy.BoundedProxyServer(("127.0.0.1", 0), s3_proxy.S3ProxyHandler)
        s3_proxy.TARGET_HOST = "127.0.0.1"
        s3_proxy.TARGET_PORT = cast(tuple[str, int], backend.server_address)[1]
        backend_thread = threading.Thread(target=backend.serve_forever, daemon=True)
        proxy_thread = threading.Thread(target=proxy_server.serve_forever, daemon=True)
        backend_thread.start()
        proxy_thread.start()
        try:
            client = socket.create_connection(cast(tuple[str, int], proxy_server.server_address), timeout=5)
            with client:
                client.settimeout(5)
                client.sendall(request)
                client.shutdown(socket.SHUT_WR)
                chunks: list[bytes] = []
                while block := client.recv(32768):
                    chunks.append(block)
            return backend.received, b"".join(chunks)
        finally:
            proxy_server.shutdown()
            backend.shutdown()
            proxy_server.server_close()
            backend.server_close()
            proxy_thread.join(timeout=5)
            backend_thread.join(timeout=5)

    def test_chunked_upload_preserves_signed_headers_chunk_extensions_and_trailers(self) -> None:
        request = (
            b"PUT /bucket/object?partNumber=1&uploadId=synthetic HTTP/1.1\r\n"
            b"Host: signed-s3.example:9000\r\n"
            b"Authorization: AWS4-HMAC-SHA256 synthetic-signature\r\n"
            b"Transfer-Encoding: chunked\r\n"
            b"Trailer: x-amz-checksum-crc32\r\n"
            b"Connection: keep-alive\r\n\r\n"
            b"3;fixture=bounded\r\nabc\r\n"
            b"0\r\nx-amz-checksum-crc32: synthetic\r\n\r\n"
        )
        response = (
            b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n"
            b"Trailer: x-proxy-result\r\nConnection: close\r\n\r\n"
            b"4\r\ntest\r\n0\r\nx-proxy-result: complete\r\n\r\n"
        )
        upstream_request, client_response = self._proxy_roundtrip(request, response)
        self.assertIn(b"PUT /bucket/object?partNumber=1&uploadId=synthetic HTTP/1.1\r\n", upstream_request)
        self.assertIn(b"Host: signed-s3.example:9000\r\n", upstream_request)
        self.assertIn(b"Authorization: AWS4-HMAC-SHA256 synthetic-signature\r\n", upstream_request)
        self.assertIn(b"3;fixture=bounded\r\nabc\r\n", upstream_request)
        self.assertIn(b"x-amz-checksum-crc32: synthetic\r\n\r\n", upstream_request)
        self.assertNotIn(b"Connection: keep-alive", upstream_request)
        self.assertIn(b"Connection: close\r\n", upstream_request)
        self.assertEqual(client_response, response)

    def test_content_length_upload_and_response_stream_without_reframing(self) -> None:
        request = (
            b"PUT /bucket/object HTTP/1.1\r\nHost: signed-s3.example:9000\r\n"
            b"Authorization: synthetic\r\nContent-Length: 6\r\n\r\nabcdef"
        )
        response = b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"
        upstream_request, client_response = self._proxy_roundtrip(request, response)
        self.assertIn(b"Content-Length: 6\r\n", upstream_request)
        self.assertTrue(upstream_request.endswith(b"\r\n\r\nabcdef"))
        self.assertEqual(client_response, response)


if __name__ == "__main__":
    unittest.main()
