"""HTTP probes for the PyPI client acceptance flow."""

from __future__ import annotations

import hashlib
import html.parser
import http.client
import sys
from email.message import Message
import urllib.parse
from dataclasses import dataclass
from typing import Mapping, NoReturn

HTTP_TIMEOUT_SECONDS = 45
MAX_RESPONSE_BYTES = 64 * 1024 * 1024
@dataclass(frozen=True)
class HTTPResult:
    status: int
    headers: Mapping[str, tuple[str, ...]]
    body: bytes

    def header(self, name: str) -> str:
        values = self.headers.get(name.lower(), ())
        if not values:
            return ""
        if any(value != values[0] for value in values[1:]):
            fail(f"HTTP response has conflicting {name} headers")
        return values[0]


class SimpleLinkParser(html.parser.HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.hrefs: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag.lower() != "a":
            return
        for name, value in attrs:
            if name.lower() == "href" and value:
                self.hrefs.append(value)


def fail(message: str) -> NoReturn:
    print(f"FAIL: {message}", file=sys.stderr, flush=True)
    raise SystemExit(1)


def request_http(url: str, headers: dict[str, str] | None = None, method: str = "GET") -> HTTPResult:
    parsed_url = urllib.parse.urlsplit(url)
    if parsed_url.scheme not in {"http", "https"} or not parsed_url.hostname:
        fail(f"unsupported HTTP URL: {url}")
    connection_type = http.client.HTTPSConnection if parsed_url.scheme == "https" else http.client.HTTPConnection
    connection = connection_type(parsed_url.hostname, parsed_url.port, timeout=HTTP_TIMEOUT_SECONDS)
    request_target = urllib.parse.urlunsplit(("", "", parsed_url.path or "/", parsed_url.query, ""))
    try:
        connection.request(method, request_target, headers=headers or {})
        response = connection.getresponse()
        status = response.status
        raw_headers = response.headers
        normalized_headers = response_headers(raw_headers)
        body = response.read(MAX_RESPONSE_BYTES + 1) if method != "HEAD" else b""
        if len(body) > MAX_RESPONSE_BYTES:
            fail(f"response exceeded {MAX_RESPONSE_BYTES} bytes: {url}")
        return HTTPResult(status, normalized_headers, body)
    except (http.client.HTTPException, TimeoutError, OSError) as exc:
        fail(f"HTTP request failed for {url}: {exc}")
    finally:
        connection.close()


def response_headers(headers: Message) -> Mapping[str, tuple[str, ...]]:
    values_by_name: dict[str, list[str]] = {}
    for name, value in headers.items():
        values_by_name.setdefault(name.lower(), []).append(value)
    return {name: tuple(values) for name, values in values_by_name.items()}


def request_bytes(url: str, headers: dict[str, str] | None = None, method: str = "GET") -> HTTPResult:
    return request_http(url, headers, method)


def six_wheel_url(index_url: str, base: urllib.parse.SplitResult, alias: str, filename: str) -> tuple[str, str, str]:
    index = request_bytes(index_url + "six/")
    if index.status != 200:
        fail(f"six simple index returned HTTP {index.status}")
    parser = SimpleLinkParser()
    parser.feed(index.body.decode("utf-8", errors="replace"))
    for href in parser.hrefs:
        resolved = urllib.parse.urljoin(index_url + "six/", href)
        parsed = urllib.parse.urlsplit(resolved)
        wheel_name = urllib.parse.unquote(parsed.path.rsplit("/", 1)[-1])
        hash_name, separator, expected_hash = parsed.fragment.partition("=")
        if wheel_name != filename or not separator or hash_name.lower() != "sha256":
            continue
        if len(expected_hash) != 64 or any(ch not in "0123456789abcdefABCDEF" for ch in expected_hash):
            fail("six simple index contains an invalid wheel SHA-256 fragment")
        if parsed.scheme != base.scheme or parsed.netloc.lower() != base.netloc.lower():
            fail("six wheel link escaped the configured RegiMux origin")
        if not parsed.path.startswith(f"/pypi/{alias}/packages/"):
            fail(f"six wheel link did not use the PyPI proxy route: {parsed.path}")
        request_url = urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, parsed.path, parsed.query, ""))
        return request_url, expected_hash.lower(), resolved
    fail(f"six simple index did not contain {filename} with a SHA-256 fragment")


def digest(body: bytes) -> str:
    return hashlib.sha256(body).hexdigest()


def require_status(result: HTTPResult, status: int, label: str) -> None:
    if result.status != status:
        fail(f"{label} returned HTTP {result.status}, expected {status}")


def verify_cached_ranges(url: str, expected_hash: str, first_wheel: bytes, second_wheel: bytes) -> int:
    if digest(first_wheel) != expected_hash or digest(second_wheel) != expected_hash or first_wheel != second_wheel:
        fail("pip downloads did not match the simple-index SHA-256 or each other")

    full = request_bytes(url)
    require_status(full, 200, "cached full wheel GET")
    if full.header("X-Mirror-Cache").lower() != "hit":
        fail(f"cached wheel GET X-Mirror-Cache={full.header('X-Mirror-Cache')!r}, expected 'hit'")
    if digest(full.body) != expected_hash or full.body != first_wheel:
        fail("cached full wheel body does not match the verified pip wheel")
    total = len(full.body)

    head = request_bytes(url, method="HEAD")
    require_status(head, 200, "cached wheel HEAD")
    if head.header("X-Mirror-Cache").lower() != "hit":
        fail("cached wheel HEAD did not report X-Mirror-Cache: hit")
    if head.header("Content-Length") != str(total) or head.header("Accept-Ranges").lower() != "bytes":
        fail("cached wheel HEAD did not report the complete size and byte-range support")

    partial = request_bytes(url, {"Range": "bytes=0-0"})
    require_status(partial, 206, "cached wheel first-byte range")
    if partial.header("Content-Range") != f"bytes 0-0/{total}" or partial.body != first_wheel[:1]:
        fail("cached wheel first-byte range returned an incorrect total or body")

    validator = head.header("ETag")
    if not validator or validator.startswith("W/"):
        validator = head.header("Last-Modified")
    if not validator:
        fail("cached wheel HEAD supplied no strong ETag or Last-Modified validator for If-Range")
    conditional = request_bytes(url, {"Range": "bytes=1-3", "If-Range": validator})
    require_status(conditional, 206, "matching If-Range")
    if conditional.header("Content-Range") != f"bytes 1-3/{total}" or conditional.body != first_wheel[1:4]:
        fail("matching If-Range returned an incorrect range")

    mismatch = '"regimux-acceptance-mismatch"'
    if mismatch == validator:
        mismatch = '"regimux-acceptance-other"'
    ignored_range = request_bytes(url, {"Range": "bytes=1-3", "If-Range": mismatch})
    require_status(ignored_range, 200, "mismatched If-Range")
    if ignored_range.body != first_wheel:
        fail("mismatched If-Range did not return the complete wheel")

    unsatisfiable = request_bytes(url, {"Range": f"bytes={total}-"})
    require_status(unsatisfiable, 416, "unsatisfiable wheel range")
    if unsatisfiable.header("Content-Range") != f"bytes */{total}":
        fail("unsatisfiable range did not report the complete representation size")

    after_ranges = request_bytes(url)
    require_status(after_ranges, 200, "full wheel GET after ranges")
    if after_ranges.header("X-Mirror-Cache").lower() != "hit":
        fail("full wheel GET after ranges did not remain a cache hit")
    if after_ranges.body != first_wheel or digest(after_ranges.body) != expected_hash:
        fail("range requests changed or truncated the complete cached wheel")
    return total
