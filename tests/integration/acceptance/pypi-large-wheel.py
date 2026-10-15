"""Verify large-wheel streaming and cold-range cache publication boundaries."""

from __future__ import annotations

import hashlib
import http.client
import os
import time
import urllib.parse
from typing import cast

import psycopg
from botocore.config import Config
from botocore.session import get_session
from mypy_boto3_s3 import S3Client
from psycopg.rows import DictRow

from cleanup_live_db import head_object, pg_connect
from pypi_acceptance_http import request_bytes

WHEEL_PATH = (
    "/packages/ec/2d/04d1871e2e3dc8d4d04a3439e5286d55ae64e31a514ae12e95f3f4c9dfa2/"
    "deepseek_harness_runtime_bin-0.1.5rc1-py3-none-macosx_14_0_arm64.whl"
)
FILENAME = WHEEL_PATH.rsplit("/", 1)[-1]
REFERENCE = "https/files.pythonhosted.org" + WHEEL_PATH
EXPECTED_SIZE = 75_545_685
EXPECTED_SHA256 = "65f20c541a499f08c36ac3bfdafce350ca8c7cc23488c1faf4d9b9524b547156"
RANGE_SIZE = 65_536


def require(condition: bool, message: str) -> None:
    if not condition:
        raise RuntimeError(message)


def artifact_count(conn: psycopg.Connection[DictRow]) -> int:
    row = conn.execute(
        "SELECT count(*) AS count FROM meta_tags "
        "WHERE alias=%s AND repository=%s AND reference=%s",
        (os.getenv("REGIMUX_PYPI_ALIAS", "default"), "pypi/packages", REFERENCE),
    ).fetchone()
    if row is None:
        raise RuntimeError("Postgres returned no artifact-count row")
    return int(row["count"])


def blob_count(conn: psycopg.Connection[DictRow], digest: str) -> int:
    row = conn.execute("SELECT count(*) AS count FROM meta_blobs WHERE digest=%s", (digest,)).fetchone()
    if row is None:
        raise RuntimeError("Postgres returned no blob-count row")
    return int(row["count"])


def stream_full_wheel(url: str) -> tuple[int, str, bytes, str]:
    parsed = urllib.parse.urlsplit(url)
    host = parsed.hostname
    if parsed.scheme not in {"http", "https"} or host is None:
        raise RuntimeError("invalid RegiMux URL")
    if parsed.scheme == "https":
        connection: http.client.HTTPConnection = http.client.HTTPSConnection(
            host, parsed.port, timeout=60
        )
    else:
        connection = http.client.HTTPConnection(host, parsed.port, timeout=60)
    target = urllib.parse.urlunsplit(("", "", parsed.path or "/", parsed.query, ""))
    digest = hashlib.sha256()
    total = 0
    tail = bytearray()
    cache_header = ""
    try:
        connection.request(
            "GET",
            target,
            headers={"Accept-Encoding": "identity", "User-Agent": "regimux-large-wheel-acceptance"},
        )
        response = connection.getresponse()
        require(response.status == 200, f"full wheel GET returned HTTP {response.status}")
        cache_header = response.getheader("X-Mirror-Cache", "") or ""
        while chunk := response.read(1024 * 1024):
            total += len(chunk)
            require(total <= EXPECTED_SIZE, f"full wheel exceeded expected size {EXPECTED_SIZE}")
            digest.update(chunk)
            tail.extend(chunk)
            if len(tail) > RANGE_SIZE:
                del tail[:-RANGE_SIZE]
    finally:
        connection.close()
    return total, digest.hexdigest(), bytes(tail), cache_header


def main() -> None:
    base = os.environ["REGIMUX_BASE_URL"].rstrip("/")
    alias = os.getenv("REGIMUX_PYPI_ALIAS", "default")
    identity = urllib.parse.urlencode(
        {
            "__regimux_project": "deepseek-harness-runtime-bin",
            "__regimux_sha256": EXPECTED_SHA256,
        }
    )
    url = (
        f"{base}/pypi/{urllib.parse.quote(alias, safe='')}/packages/https/files.pythonhosted.org"
        f"{WHEEL_PATH}?{identity}"
    )
    s3 = cast(
        S3Client,
        get_session().create_client(
            "s3",
            endpoint_url=os.environ["S3_ENDPOINT"],
            config=Config(signature_version="s3v4", s3={"addressing_style": "path"}),
        ),
    )

    with pg_connect() as conn:
        require(artifact_count(conn) == 0, "large-wheel test requires an uncached artifact")
        ranged = request_bytes(url, {"Range": f"bytes=-{RANGE_SIZE}"})
        require(ranged.status == 206, f"cold suffix Range returned HTTP {ranged.status}")
        expected_range = f"bytes {EXPECTED_SIZE - RANGE_SIZE}-{EXPECTED_SIZE - 1}/{EXPECTED_SIZE}"
        require(
            ranged.header("Content-Range") == expected_range,
            f"unexpected cold Content-Range {ranged.header('Content-Range')!r}",
        )
        require(len(ranged.body) == RANGE_SIZE, f"cold Range returned {len(ranged.body)} bytes, expected {RANGE_SIZE}")
        require(ranged.header("Accept-Ranges").lower() == "bytes", "cold Range did not advertise byte support")
        require(ranged.header("X-Mirror-Cache").lower() != "hit", "cold Range unexpectedly came from cache")
        partial_digest = "sha256:" + hashlib.sha256(ranged.body).hexdigest()
        time.sleep(2)
        require(artifact_count(conn) == 0, "cold Range published an artifact tag")
        require(blob_count(conn, partial_digest) == 0, "cold Range published a partial blob row")
        require(head_object(s3, partial_digest) is None, "cold Range published a partial S3 object")

        size, digest, expected_tail, cache_header = stream_full_wheel(url)
        require(size == EXPECTED_SIZE, f"full wheel size was {size}, expected {EXPECTED_SIZE}")
        require(digest == EXPECTED_SHA256, f"full wheel SHA-256 was {digest}, expected {EXPECTED_SHA256}")
        require(cache_header.lower() != "hit", "full wheel request was expected to populate the cold cache")
        deadline = time.monotonic() + 120
        while artifact_count(conn) == 0:
            require(time.monotonic() < deadline, "full wheel cache metadata was not published")
            time.sleep(1)

        head = request_bytes(url, method="HEAD")
        require(head.status == 200, f"cached HEAD returned HTTP {head.status}")
        require(head.header("X-Mirror-Cache").lower() == "hit", "cached HEAD did not report a cache hit")
        require(head.header("Content-Length") == str(EXPECTED_SIZE), "cached HEAD reported an incorrect wheel size")
        hot_range = request_bytes(url, {"Range": f"bytes=-{RANGE_SIZE}"})
        require(hot_range.status == 206, f"hot suffix Range returned HTTP {hot_range.status}")
        require(hot_range.header("X-Mirror-Cache").lower() == "hit", "hot Range did not report a cache hit")
        require(hot_range.header("Content-Range") == expected_range, "hot Range reported an incorrect total or range")
        require(hot_range.body == expected_tail, "hot Range bytes differ from the verified full wheel")

    object_info = head_object(s3, "sha256:" + EXPECTED_SHA256)
    if object_info is None:
        raise RuntimeError("complete wheel CAS object is missing from S3")
    require(
        object_info["size"] == EXPECTED_SIZE,
        f"S3 wheel size was {object_info['size']}, expected {EXPECTED_SIZE}",
    )
    print(f"PASS: cold Range did not publish partial data; streamed {FILENAME} SHA-256 and verified hot cache/S3", flush=True)


if __name__ == "__main__":
    main()
