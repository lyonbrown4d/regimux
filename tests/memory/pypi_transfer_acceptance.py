"""Stream the pinned issue wheel, verify recovery/cache, and run real uv/pip."""
from __future__ import annotations

import concurrent.futures
import hashlib
import http.client
import json
import os
import resource
import shutil
import sqlite3
import subprocess
import sys
import time
import urllib.parse
from dataclasses import dataclass
from pathlib import Path
from typing import cast

from pypi_transfer_fixture import (
    CHUNK_SIZE, DEMO_NAME, ETAG, PREFIX_SIZE, WHEEL_NAME, WHEEL_SHA256, WHEEL_SIZE, file_digest,
)

BASE = os.getenv("REGIMUX_BASE_URL", "http://regimux:8080").rstrip("/")
SOURCE = os.getenv("PYPI_FIXTURE_URL", "http://fixture:8081")
REPORT = Path(os.getenv("REPORT_DIR", "/reports"))
CACHE = Path(os.getenv("CACHE_ROOT", "/cache"))


def require(condition: bool, message: str) -> None:
    if not condition:
        raise RuntimeError(message)


def connection(url: str) -> tuple[http.client.HTTPConnection, str]:
    parsed = urllib.parse.urlsplit(url)
    require(parsed.scheme == "http" and parsed.hostname is not None, "acceptance requires a private HTTP URL")
    target = urllib.parse.urlunsplit(("", "", parsed.path or "/", parsed.query, ""))
    return http.client.HTTPConnection(parsed.hostname or "", parsed.port, timeout=20), target


def request(url: str, headers: dict[str, str] | None = None, method: str = "GET") -> tuple[int, dict[str, str], bytes]:
    client, target = connection(url)
    try:
        client.request(method, target, headers=headers or {})
        response = client.getresponse()
        data = b"" if method == "HEAD" else response.read(1 << 20)
        require(len(data) < 1 << 20, "bounded HTTP probe exceeded 1MiB")
        return response.status, dict(response.getheaders()), data
    finally:
        client.close()


def json_object(data: bytes) -> dict[str, object]:
    value: object = json.loads(data)
    if not isinstance(value, dict):
        raise TypeError("expected a JSON object with string keys")
    entries = cast(dict[object, object], value)
    if not all(isinstance(key, str) for key in entries):
        raise TypeError("JSON object keys must be strings")
    return {key: item for key, item in entries.items() if isinstance(key, str)}


def source_state() -> dict[str, object]:
    status, _, data = request(SOURCE + "/__state")
    require(status == 200, f"fixture state returned {status}")
    return json_object(data)


@dataclass(frozen=True)
class StreamResult:
    size: int
    sha256: str
    seconds: float
    prefix: bytes
    suffix: bytes


def stream_wheel(url: str, expected_cache: str, headers: dict[str, str] | None = None) -> StreamResult:
    client, target = connection(url)
    digest, total, prefix, suffix = hashlib.sha256(), 0, bytearray(), bytearray()
    started = time.monotonic()
    try:
        client.request("GET", target, headers={"Accept-Encoding": "identity", **(headers or {})})
        response = client.getresponse()
        require(response.status == 200, f"whole wheel status={response.status}")
        require(response.getheader("X-Mirror-Cache", "").lower() == expected_cache, "unexpected wheel cache status")
        require(response.getheader("Content-Length") == str(WHEEL_SIZE), "incorrect whole wheel Content-Length")
        print(f"WHOLE headers status=200 cache={expected_cache} elapsed={time.monotonic()-started:.3f}s", flush=True)
        while chunk := response.read(CHUNK_SIZE):
            digest.update(chunk)
            total += len(chunk)
            require(total <= WHEEL_SIZE, "whole wheel exceeded its pinned size")
            if len(prefix) < CHUNK_SIZE:
                prefix.extend(chunk[:CHUNK_SIZE-len(prefix)])
            suffix.extend(chunk)
            del suffix[:-CHUNK_SIZE]
        actual = digest.hexdigest()
        require(total == WHEEL_SIZE and actual == WHEEL_SHA256, f"whole wheel bytes={total} sha256={actual}")
        elapsed = time.monotonic() - started
        print(f"WHOLE PASS bytes={total} sha256={actual} elapsed={elapsed:.3f}s", flush=True)
        return StreamResult(total, actual, elapsed, bytes(prefix), bytes(suffix))
    finally:
        client.close()


def assert_no_partial(state: dict[str, object]) -> None:
    prefix_hash = state.get("prefix_sha256")
    require(isinstance(prefix_hash, str) and len(prefix_hash) == 64, "fixture prefix digest is missing")
    digest = str(prefix_hash)
    with sqlite3.connect(f"file:{CACHE}/meta.db?mode=ro", uri=True, timeout=10) as db:
        tags = db.execute("SELECT count(*) FROM meta_tags WHERE reference LIKE ?", ("%/" + WHEEL_NAME,)).fetchone()
        blobs = db.execute("SELECT count(*) FROM meta_blobs WHERE digest=?", ("sha256:" + digest,)).fetchone()
        require(tags == (0,) and blobs == (0,), f"partial transfer published metadata tags={tags} blobs={blobs}")
    for value in (digest, WHEEL_SHA256):
        path = CACHE / "objects" / "blobs" / "sha256" / value[:2] / value
        require(not path.exists(), f"incomplete transfer published CAS object {value}")
    print("PARTIAL PASS: no wheel tag, partial blob row, or partial/full CAS publication during idle", flush=True)


def cold_transfer(url: str) -> StreamResult:
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
        future = pool.submit(stream_wheel, url, "miss")
        deadline = time.monotonic() + 20
        state = source_state()
        while state.get("idle_started") is not True:
            require(time.monotonic() < deadline and not future.done(), "source never entered its idle phase")
            time.sleep(0.25)
            state = source_state()
        assert_no_partial(state)
        return future.result(timeout=100)


def cached_ranges(url: str, wheel: StreamResult) -> None:
    status, headers, data = request(url, method="HEAD")
    require(status == 200 and not data and headers.get("Content-Length") == str(WHEEL_SIZE), "cached HEAD is incorrect")
    require(headers.get("X-Mirror-Cache", "").lower() == "hit", "HEAD missed the completed cache")
    cases = [
        ({"Range": f"bytes=0-{CHUNK_SIZE-1}"}, wheel.prefix, f"bytes 0-{CHUNK_SIZE-1}/{WHEEL_SIZE}"),
        ({"Range": f"bytes=-{CHUNK_SIZE}"}, wheel.suffix, f"bytes {WHEEL_SIZE-CHUNK_SIZE}-{WHEEL_SIZE-1}/{WHEEL_SIZE}"),
        ({"Range": "bytes=0-2", "If-Range": ETAG}, wheel.prefix[:3], f"bytes 0-2/{WHEEL_SIZE}"),
    ]
    for selected, expected, content_range in cases:
        status, headers, data = request(url, selected)
        require(status == 206 and data == expected and headers.get("Content-Range") == content_range, "cached range bytes/offset failed")
        require(headers.get("X-Mirror-Cache", "").lower() == "hit", "range missed completed cache")
    status, headers, data = request(url, {"Range": f"bytes={WHEEL_SIZE}-"})
    require(status == 416 and not data and headers.get("Content-Range") == f"bytes */{WHEEL_SIZE}", "unsatisfiable range failed")
    stream_wheel(url, "hit", {"Range": "bytes=0-2", "If-Range": '"changed"'})
    print("RANGE PASS: cached HEAD/prefix/suffix/If-Range match+mismatch/416", flush=True)


def run_clients(index: str) -> None:
    uv = shutil.which("uv")
    require(uv is not None, "uv must be present; acceptance cannot skip real clients")
    project, downloads = REPORT / "uv-project", REPORT / "pip-downloads"
    project.mkdir(parents=True)
    downloads.mkdir()
    (project / "pyproject.toml").write_text(
        '[project]\nname="transfer-acceptance"\nversion="1.0.0"\nrequires-python=">=3.12"\ndependencies=["regimux-demo==1.0.0"]\n', encoding="utf-8"
    )
    env = {key: value for key, value in os.environ.items()
           if not key.upper().startswith(("UV_", "PIP_")) and key.upper() not in {"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"}}
    env.update({"UV_NO_CONFIG": "1", "UV_PYTHON_DOWNLOADS": "never", "UV_CACHE_DIR": str(REPORT / "uv-cache"),
                "UV_INSECURE_HOST": "regimux", "PIP_CONFIG_FILE": os.devnull, "PIP_DISABLE_PIP_VERSION_CHECK": "1"})
    commands = [
        [str(uv), "lock", "--default-index", index, "--python", sys.executable],
        [str(uv), "lock", "--check", "--default-index", index, "--python", sys.executable],
        [str(uv), "sync", "--locked", "--no-install-project", "--default-index", index, "--python", sys.executable],
        [str(uv), "run", "--no-sync", "python", "-c",
         "import regimux_demo; assert regimux_demo.__version__ == '1.0.0'"],
        [sys.executable, "-m", "pip", "download", "--no-deps", "--no-cache-dir", "--only-binary=:all:",
         "--index-url", index, "--trusted-host", "regimux", "--dest", str(downloads), "regimux-demo==1.0.0"],
        [sys.executable, "-m", "pip", "download", "--no-deps", "--no-cache-dir", "--only-binary=:all:",
         "--platform", "macosx_14_0_arm64", "--python-version", "3.12", "--index-url", index,
         "--trusted-host", "regimux", "--dest", str(downloads), "deepseek-harness-runtime-bin==0.1.5rc1"],
    ]
    for number, command in enumerate(commands):
        print("CLIENT " + " ".join(command), flush=True)
        result = subprocess.run(command, cwd=project, env=env, capture_output=True, text=True, timeout=180, check=False)
        (REPORT / f"client-{number}.log").write_text(result.stdout + result.stderr, encoding="utf-8")
        require(result.returncode == 0, f"real client failed: {command}\n{result.stdout}\n{result.stderr}")
    require((project / "uv.lock").stat().st_size > 0 and (downloads / DEMO_NAME).is_file(), "uv lock or demo pip artifact missing")
    require(file_digest(downloads / WHEEL_NAME) == WHEEL_SHA256, "pip original wheel SHA-256 mismatch")


def verify_source(state: dict[str, object]) -> list[dict[str, object]]:
    require(state.get("idle_canceled") is True, "original idle attempt was not canceled")
    raw = state.get("requests")
    require(isinstance(raw, list), "source request history missing")
    events = [json_object(json.dumps(item).encode()) for item in cast(list[object], raw)]
    original = [event for event in events if event.get("original") is True]
    require(len(original) == 2, f"expected one cold GET and one resume, got {original}")
    resumed = original[1]
    require(resumed.get("status") == 206 and resumed.get("start") == PREFIX_SIZE, f"wrong recovery offset: {resumed}")
    require(resumed.get("end") == WHEEL_SIZE-1 and resumed.get("if_range") == ETAG, f"wrong recovery identity: {resumed}")
    require(sum(int(str(event["sent"])) for event in original) == WHEEL_SIZE, "source recovery duplicated or lost bytes")
    require(any(event.get("original") is False and event.get("status") == 206 for event in events), "uv never exercised demo wheel Range")
    return events


def main() -> None:
    REPORT.mkdir(parents=True, exist_ok=True)
    deadline = time.monotonic() + 90
    while True:
        try:
            require(request(BASE + "/livez")[0] == 200, "app not live")
            break
        except (OSError, http.client.HTTPException, RuntimeError):
            require(time.monotonic() < deadline, "RegiMux did not become healthy")
            time.sleep(1)
    index = BASE + "/pypi/default/simple/"
    query = urllib.parse.urlencode({"__regimux_project": "deepseek-harness-runtime-bin", "__regimux_sha256": WHEEL_SHA256})
    url = BASE + "/pypi/default/packages/http/fixture:8081/packages/" + WHEEL_NAME + "?" + query
    cold = cold_transfer(url)
    require(10 <= cold.seconds < 20, "cold wheel did not recover before the client idle deadline")
    stream_wheel(url, "hit")
    cached_ranges(url, cold)
    run_clients(index)
    events = verify_source(source_state())
    report = {"passed": True, "wheel": WHEEL_NAME, "bytes": cold.size, "sha256": cold.sha256,
              "cold_seconds": cold.seconds, "recoveries": 1, "source_requests": events,
              "client_max_rss_kib": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss}
    (REPORT / "pypi-transfer.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report), flush=True)


if __name__ == "__main__":
    main()
