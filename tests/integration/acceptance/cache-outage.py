"""Check that a real S3 outage does not reject healthy upstream downloads."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import time
from pathlib import Path
from typing import TypedDict

import psycopg

from pypi_acceptance_http import request_bytes


class Download(TypedDict):
    label: str
    url: str
    sha256: str


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("prepare", "verify"))
    args = parser.parse_args()
    reports = Path(os.getenv("REPORT_DIR", "/reports"))
    snapshot = reports / "cache-outage-snapshot.json"
    base = os.environ["REGIMUX_BASE_URL"].rstrip("/")
    if args.mode == "prepare":
        wheel: dict[str, str] = json.loads((reports / "pypi-wheel.json").read_text())
        artifacts: list[Download] = []
        for label, url in (
            ("maven-hot", base + "/maven/central/commons-io/commons-io/2.16.1/commons-io-2.16.1.jar"),
            ("pypi-hot", wheel["url"]),
        ):
            result = request_bytes(url)
            assert result.status == 200, (label, result.status)
            artifacts.append(Download(label=label, url=url, sha256=hashlib.sha256(result.body).hexdigest()))
        snapshot.write_text(json.dumps(artifacts, indent=2), encoding="utf-8")
        print("PREPARED: warmed Maven and PyPI artifacts for S3 outage", flush=True)
        return

    artifacts: list[Download] = json.loads(snapshot.read_text())
    timings: dict[str, float] = {}
    for artifact in artifacts:
        started = time.monotonic()
        result = request_bytes(artifact["url"])
        elapsed = time.monotonic() - started
        assert result.status == 200, (artifact["label"], result.status)
        assert hashlib.sha256(result.body).hexdigest() == artifact["sha256"], artifact["label"]
        assert elapsed < 45, (artifact["label"], elapsed)
        timings[artifact["label"]] = round(elapsed, 3)
    with psycopg.connect(os.environ["PG_DSN"]) as conn:
        row = conn.execute(
            "SELECT count(*) FROM meta_tags WHERE alias=%s AND repository=%s AND reference=%s",
            ("central", "commons-codec/commons-codec/1.17.1", "commons-codec-1.17.1.pom"),
        ).fetchone()
        assert row is not None and row[0] == 0, "outage cold Maven case is already cached"
    started = time.monotonic()
    cold = request_bytes(base + "/maven/central/commons-codec/commons-codec/1.17.1/commons-codec-1.17.1.pom")
    assert cold.status == 200, cold.status
    assert b"<project" in cold.body and b"commons-codec" in cold.body
    timings["maven-cold"] = round(time.monotonic() - started, 3)
    (reports / "cache-outage-result.json").write_text(json.dumps(timings, indent=2), encoding="utf-8")
    print(f"PASS: S3 outage preserved Maven/PyPI bytes and cold Maven download; seconds={timings}", flush=True)


if __name__ == "__main__":
    main()
