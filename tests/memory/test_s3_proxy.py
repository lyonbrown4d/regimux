"""Protocol-preservation checks for the memory acceptance S3 proxy."""
from __future__ import annotations

import io
import socket
import threading
import time
import unittest
from http.client import HTTPConnection
from typing import cast

from test_s3_proxy_support import BackendServer, s3_proxy


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
            b"Connection: keep-alive\r\nKeep-Alive: timeout=60\r\n\r\n"
            b"3;fixture=bounded\r\nabc\r\n"
            b"0\r\nx-amz-checksum-crc32: synthetic\r\n\r\n"
        )
        response = (
            b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n"
            b"Trailer: x-proxy-result\r\nConnection: keep-alive\r\nKeep-Alive: timeout=60\r\n\r\n"
            b"4\r\ntest\r\n0\r\nx-proxy-result: complete\r\n\r\n"
        )
        upstream_request, client_response = self._proxy_roundtrip(request, response)
        self.assertIn(b"PUT /bucket/object?partNumber=1&uploadId=synthetic HTTP/1.1\r\n", upstream_request)
        self.assertIn(b"Host: signed-s3.example:9000\r\n", upstream_request)
        self.assertIn(b"Authorization: AWS4-HMAC-SHA256 synthetic-signature\r\n", upstream_request)
        self.assertIn(b"3;fixture=bounded\r\nabc\r\n", upstream_request)
        self.assertIn(b"x-amz-checksum-crc32: synthetic\r\n\r\n", upstream_request)
        self.assertNotIn(b"Connection: keep-alive", upstream_request)
        self.assertNotIn(b"Keep-Alive:", upstream_request)
        self.assertIn(b"Connection: close\r\n", upstream_request)
        expected_response = (
            b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n"
            b"Trailer: x-proxy-result\r\nConnection: close\r\n\r\n"
            b"4\r\ntest\r\n0\r\nx-proxy-result: complete\r\n\r\n"
        )
        self.assertEqual(client_response, expected_response)
        # Admission may split a header or consume part of the chunked body.
        # Keep the peer open: replay must not wait for EOF or a full read buffer.
        for prefix_size in (request.index(b"Host:") + 3, request.index(b"\r\n\r\n") + 7):
            with self.subTest(prefix_size=prefix_size):
                receiver, sender = socket.socketpair()
                with receiver, sender:
                    receiver.settimeout(1.5)
                    source = receiver.makefile("rb")
                    with io.BufferedReader(s3_proxy._PrefixReader(request[:prefix_size], source)) as replay:
                        sender.sendall(request[prefix_size:])
                        self.assertEqual(replay.read(len(request)), request)
                    self.assertTrue(source.closed, "the prefix reader must own its socket file")

    def test_content_length_upload_and_response_stream_without_reframing(self) -> None:
        request = (
            b"PUT /bucket/object HTTP/1.1\r\nHost: signed-s3.example:9000\r\n"
            b"Authorization: synthetic\r\nContent-Length: 6\r\n\r\nabcdef"
        )
        response = b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: keep-alive\r\nKeep-Alive: timeout=60\r\n\r\nok"
        upstream_request, client_response = self._proxy_roundtrip(request, response)
        self.assertIn(b"Content-Length: 6\r\n", upstream_request)
        self.assertTrue(upstream_request.endswith(b"\r\n\r\nabcdef"))
        self.assertEqual(
            client_response,
            b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok",
        )

    def test_idle_keepalive_clients_do_not_starve_next_proxy_request(self) -> None:
        response = b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"
        backend = BackendServer(response)
        proxy_server = s3_proxy.BoundedProxyServer(("127.0.0.1", 0), s3_proxy.S3ProxyHandler)
        old_target = (s3_proxy.TARGET_HOST, s3_proxy.TARGET_PORT)
        s3_proxy.TARGET_HOST = "127.0.0.1"
        s3_proxy.TARGET_PORT = cast(tuple[str, int], backend.server_address)[1]
        backend_thread = threading.Thread(target=backend.serve_forever, daemon=True)
        proxy_thread = threading.Thread(target=proxy_server.serve_forever, daemon=True)
        backend_thread.start()
        proxy_thread.start()
        proxy_address = cast(tuple[str, int], proxy_server.server_address)
        idle_clients: list[HTTPConnection] = []
        next_client: HTTPConnection | None = None
        try:
            for _ in range(64):
                client = HTTPConnection(*proxy_address, timeout=1.5)
                idle_clients.append(client)
                client.request("GET", "/bucket/object", headers={"Connection": "keep-alive"})
                result = client.getresponse()
                self.assertEqual(result.status, 200)
                self.assertEqual(result.read(), b"ok")

            next_client = HTTPConnection(*proxy_address, timeout=1.5)
            started = time.monotonic()
            next_client.request("GET", "/bucket/object", headers={"Connection": "keep-alive"})
            result = next_client.getresponse()
            self.assertEqual(result.status, 200)
            self.assertEqual(result.getheader("Connection"), "close")
            self.assertEqual(result.read(), b"ok")
            self.assertLess(time.monotonic() - started, 1.5)
        except (TimeoutError, socket.timeout) as error:
            self.fail(f"the next request was starved by idle HTTP/1.1 clients: {error}")
        finally:
            if next_client is not None:
                next_client.close()
            for client in idle_clients:
                client.close()
            proxy_server.shutdown()
            backend.shutdown()
            proxy_server.server_close()
            backend.server_close()
            proxy_thread.join(timeout=5)
            backend_thread.join(timeout=5)
            s3_proxy.TARGET_HOST, s3_proxy.TARGET_PORT = old_target
            self.assertFalse(proxy_thread.is_alive(), "proxy server thread did not stop")
            self.assertFalse(backend_thread.is_alive(), "backend server thread did not stop")

    def test_health_response_closes_client_connection(self) -> None:
        backend = BackendServer(b"")
        proxy_server = s3_proxy.BoundedProxyServer(("127.0.0.1", 0), s3_proxy.S3ProxyHandler)
        old_target = (s3_proxy.TARGET_HOST, s3_proxy.TARGET_PORT)
        s3_proxy.TARGET_HOST = "127.0.0.1"
        s3_proxy.TARGET_PORT = cast(tuple[str, int], backend.server_address)[1]
        backend_thread = threading.Thread(target=backend.serve_forever, daemon=True)
        proxy_thread = threading.Thread(target=proxy_server.serve_forever, daemon=True)
        backend_thread.start()
        proxy_thread.start()
        try:
            with socket.create_connection(cast(tuple[str, int], proxy_server.server_address), timeout=2) as client:
                client.settimeout(2)
                client.sendall(b"GET /__healthz HTTP/1.1\r\nHost: localhost\r\n\r\n")
                response = bytearray()
                while block := client.recv(4096):
                    response.extend(block)
            self.assertIn(b"Connection: close\r\n", response)
            self.assertTrue(response.endswith(b"ok\n"))
        finally:
            proxy_server.shutdown()
            backend.shutdown()
            proxy_server.server_close()
            backend.server_close()
            proxy_thread.join(timeout=5)
            backend_thread.join(timeout=5)
            s3_proxy.TARGET_HOST, s3_proxy.TARGET_PORT = old_target
            self.assertFalse(proxy_thread.is_alive(), "proxy server thread did not stop")
            self.assertFalse(backend_thread.is_alive(), "backend server thread did not stop")

    def test_bare_tcp_clients_do_not_starve_proxy_or_health(self) -> None:
        self._assert_idle_tcp_clients_do_not_starve_proxy_or_health(b"")

    def test_partial_headers_do_not_starve_proxy_or_health(self) -> None:
        self._assert_idle_tcp_clients_do_not_starve_proxy_or_health(
            b"GET /bucket/object HTTP/1.1\r\nHost: localhost\r\n"
        )

    def _assert_idle_tcp_clients_do_not_starve_proxy_or_health(self, partial_header: bytes) -> None:
        condition = threading.Condition()
        accepted_connections = 0
        active_handlers = 0

        class ObservedServer(s3_proxy.BoundedProxyServer):
            def process_request(self, request: socket.socket, client_address: tuple[str, int]) -> None:
                nonlocal accepted_connections
                super().process_request(request, client_address)
                with condition:
                    accepted_connections += 1
                    condition.notify_all()

        class ObservedHandler(s3_proxy.S3ProxyHandler):
            def setup(self) -> None:
                nonlocal active_handlers
                super().setup()
                with condition:
                    active_handlers += 1
                    condition.notify_all()

            def finish(self) -> None:
                nonlocal active_handlers
                try:
                    super().finish()
                finally:
                    with condition:
                        active_handlers -= 1
                        condition.notify_all()

        backend = BackendServer(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
        proxy_server = ObservedServer(("127.0.0.1", 0), ObservedHandler)
        old_target = (s3_proxy.TARGET_HOST, s3_proxy.TARGET_PORT)
        s3_proxy.TARGET_HOST = "127.0.0.1"
        s3_proxy.TARGET_PORT = cast(tuple[str, int], backend.server_address)[1]
        backend_thread = threading.Thread(target=backend.serve_forever, daemon=True)
        proxy_thread = threading.Thread(target=proxy_server.serve_forever, daemon=True)
        backend_thread.start()
        proxy_thread.start()
        proxy_address = cast(tuple[str, int], proxy_server.server_address)
        idle_clients: list[socket.socket] = []
        next_client: HTTPConnection | None = None
        try:
            # Every socket remains idle before its HTTP header block is complete.
            for _ in range(64):
                idle_client = socket.create_connection(proxy_address, timeout=1.5)
                idle_clients.append(idle_client)
                if partial_header:
                    idle_client.sendall(partial_header)
            with condition:
                self.assertTrue(
                    condition.wait_for(lambda: accepted_connections == 64, timeout=5),
                    "all 64 idle TCP connections must be accepted before the next request",
                )

            for path, expected_body in (("/__healthz", b"ok\n"), ("/bucket/object", b"ok")):
                with self.subTest(path=path):
                    next_client = HTTPConnection(*proxy_address, timeout=1.5)
                    started = time.monotonic()
                    try:
                        next_client.request("GET", path)
                        result = next_client.getresponse()
                        self.assertEqual(result.status, 200)
                        self.assertEqual(result.getheader("Connection"), "close")
                        self.assertEqual(result.read(), expected_body)
                        self.assertLess(time.monotonic() - started, 1.5)
                    except (TimeoutError, socket.timeout) as error:
                        self.fail(f"{path} was starved by 64 incomplete HTTP clients: {error}")
                    finally:
                        next_client.close()
                        next_client = None
        finally:
            if next_client is not None:
                next_client.close()
            for client in idle_clients:
                client.close()
            proxy_server.shutdown()
            backend.shutdown()
            proxy_server.server_close()
            backend.server_close()
            proxy_thread.join(timeout=5)
            backend_thread.join(timeout=5)
            s3_proxy.TARGET_HOST, s3_proxy.TARGET_PORT = old_target
            self.assertFalse(proxy_thread.is_alive(), "proxy server thread did not stop")
            self.assertFalse(backend_thread.is_alive(), "backend server thread did not stop")
            with condition:
                self.assertTrue(
                    condition.wait_for(lambda: active_handlers == 0, timeout=5),
                    "bare TCP client handlers did not stop after their sockets closed",
                )


if __name__ == "__main__":
    unittest.main()
