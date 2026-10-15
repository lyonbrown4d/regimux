"""Download and verify an actual Node distribution through the dist proxy."""
from __future__ import annotations

import hashlib
import os

from pypi_acceptance_http import request_bytes


def main() -> None:
    base = os.getenv("REGIMUX_BASE_URL", "http://regimux:8080").rstrip("/")
    version = "v20.19.0"
    filename = f"node-{version}-linux-x64.tar.xz"
    root = f"{base}/dist/nodejs/{version}"
    checksums = request_bytes(f"{root}/SHASUMS256.txt")
    assert checksums.status == 200, checksums.status
    entries = dict(line.split(maxsplit=1)[::-1] for line in checksums.body.decode().splitlines())
    first = request_bytes(f"{root}/{filename}")
    assert first.status == 200, first.status
    assert hashlib.sha256(first.body).hexdigest() == entries[filename]
    second = request_bytes(f"{root}/{filename}")
    assert second.status == 200, second.status
    assert second.header("X-Mirror-Cache").lower() == "hit", second.headers
    assert hashlib.sha256(second.body).hexdigest() == entries[filename]
    print(f"PASS: dist downloaded {filename}, verified SHA-256, and reused S3 cache", flush=True)


if __name__ == "__main__":
    main()
