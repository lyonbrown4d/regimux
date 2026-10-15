"""Verify a cold range from a real public wheel cannot publish a partial cache."""
from __future__ import annotations

import hashlib
import os
import time
import urllib.parse
from typing import cast

import psycopg
from botocore.config import Config
from botocore.session import get_session
from mypy_boto3_s3 import S3Client

from cleanup_live_db import head_object
from pypi_acceptance_http import request_bytes, six_wheel_url


def tag_count(conn: psycopg.Connection, filename: str) -> int:
    row = conn.execute(
        "SELECT count(*) FROM meta_tags WHERE split_part(reference,'?',1) LIKE %s",
        ("%/" + filename,),
    ).fetchone()
    assert row is not None
    return int(row[0])


def main() -> None:
    base = os.environ["REGIMUX_BASE_URL"].rstrip("/")
    alias = os.getenv("REGIMUX_PYPI_ALIAS", "default")
    version = os.getenv("REGIMUX_COLD_PYPI_VERSION", "1.16.0")
    filename = f"six-{version}-py2.py3-none-any.whl"
    index = f"{base}/pypi/{alias}/simple/"
    url, expected_hash, _ = six_wheel_url(index, urllib.parse.urlsplit(base), alias, filename)
    s3 = cast(S3Client, get_session().create_client(
        "s3", endpoint_url=os.environ["S3_ENDPOINT"],
        config=Config(signature_version="s3v4", s3={"addressing_style": "path"}),
    ))
    with psycopg.connect(os.environ["PG_DSN"], autocommit=True) as conn:
        assert tag_count(conn, filename) == 0, "cold test requires an uncached wheel version"
        ranged = request_bytes(url, {"Range": "bytes=-128"})
        assert ranged.status == 206, ranged.status
        assert len(ranged.body) == 128, len(ranged.body)
        assert ranged.header("Content-Range").startswith("bytes "), ranged.headers
        assert ranged.header("Accept-Ranges").lower() == "bytes", ranged.headers
        assert ranged.header("X-Mirror-Cache").lower() != "hit", ranged.headers
        partial_digest = "sha256:" + hashlib.sha256(ranged.body).hexdigest()
        time.sleep(2)
        assert tag_count(conn, filename) == 0, "partial GET published a full wheel tag"
        assert head_object(s3, partial_digest) is None, "partial GET stored an incomplete CAS object"
        full = request_bytes(url)
        assert full.status == 200, full.status
        assert len(full.body) > 128
        assert hashlib.sha256(full.body).hexdigest() == expected_hash
        deadline = time.monotonic() + 90
        while tag_count(conn, filename) == 0:
            assert time.monotonic() < deadline, "full wheel cache was not published"
            time.sleep(1)
        cached = request_bytes(url)
        assert cached.status == 200 and cached.header("X-Mirror-Cache").lower() == "hit", cached.headers
        assert hashlib.sha256(cached.body).hexdigest() == expected_hash
        assert head_object(s3, "sha256:" + expected_hash) is not None
    print(f"PASS: cold Range {filename} returned 206 without partial publication; full SHA-256/S3 cache verified", flush=True)


if __name__ == "__main__":
    main()
