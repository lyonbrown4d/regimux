"""Verify that two real RegiMux replicas share cold and hot artifact content."""
from __future__ import annotations

import hashlib
import os
from concurrent.futures import ThreadPoolExecutor

from pypi_acceptance_http import HTTPResult, request_bytes


def main() -> None:
    bases = [os.environ["REGIMUX_BASE_URL"], os.environ["REGIMUX_PEER_URL"]]
    path = "/maven/central/org/apache/commons/commons-lang3/3.17.0/commons-lang3-3.17.0.jar"
    with ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(request_bytes, (base + path for base in bases)))
    verify_equal(results)
    with ThreadPoolExecutor(max_workers=2) as pool:
        hits = list(pool.map(request_bytes, (base + path for base in bases)))
    verify_equal(hits)
    for result in hits:
        assert result.header("X-Mirror-Cache").lower() == "hit", result.headers
    print("PASS: both replicas served identical content and then shared S3 cache hits", flush=True)


def verify_equal(results: list[HTTPResult]) -> None:
    assert len(results) == 2
    assert all(result.status == 200 for result in results)
    assert all(len(result.body) > 1024 for result in results)
    assert hashlib.sha256(results[0].body).digest() == hashlib.sha256(results[1].body).digest()


if __name__ == "__main__":
    main()
