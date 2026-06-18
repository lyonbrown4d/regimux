#!/usr/bin/env python3
import hashlib
import json
import os
import sys
import threading
import time
import urllib.error
import urllib.request


REGIMUX_A = os.environ.get("REGIMUX_MULTI_A", "http://regimux-a:8080").rstrip("/")
REGIMUX_B = os.environ.get("REGIMUX_MULTI_B", "http://regimux-b:8080").rstrip("/")
UPSTREAM = os.environ.get("REGIMUX_FAKE_REGISTRY_URL", "http://fake-registry:5000").rstrip("/")
REPO = os.environ.get("REGIMUX_MULTI_REPO", "hub/library/regimux-fixture").strip("/")
TIMEOUT = float(os.environ.get("REGIMUX_MULTI_TIMEOUT_SECONDS", "60"))


class PullResult:
    def __init__(self, name, status, cache, body, elapsed):
        self.name = name
        self.status = status
        self.cache = cache
        self.body = body
        self.elapsed = elapsed


def fail(message):
    print(message, file=sys.stderr)
    sys.exit(1)


def get_json(url):
    with urllib.request.urlopen(url, timeout=TIMEOUT) as response:
        return json.loads(response.read().decode("utf-8"))


def fetch_blob(name, base_url, digest, results, done):
    started = time.monotonic()
    url = f"{base_url}/v2/{REPO}/blobs/{digest}"
    try:
        request = urllib.request.Request(url, method="GET")
        with urllib.request.urlopen(request, timeout=TIMEOUT) as response:
            body = response.read()
            results[name] = PullResult(
                name=name,
                status=response.status,
                cache=response.headers.get("X-Mirror-Cache", ""),
                body=body,
                elapsed=time.monotonic() - started,
            )
    except urllib.error.HTTPError as exc:
        results[name] = exc
    except Exception as exc:  # noqa: BLE001 - script should report exact integration failure.
        results[name] = exc
    finally:
        done.set()


def wait_for_layer_get(start_count, first_done):
    deadline = time.monotonic() + TIMEOUT
    while time.monotonic() < deadline:
        counts = get_json(f"{UPSTREAM}/__debug__/requests")
        if counts.get("layer_blob_gets", 0) > start_count:
            if first_done.is_set():
                fail("first replica completed before second request was started; fake registry stream is not slow enough")
            return counts
        if first_done.is_set():
            fail("first replica completed without opening a layer blob request upstream")
        time.sleep(0.02)
    fail("timed out waiting for first replica to open upstream layer blob request")


def join_thread(thread, name):
    thread.join(TIMEOUT)
    if thread.is_alive():
        fail(f"{name} did not finish within {TIMEOUT:.0f}s")


def require_result(results, name, want_cache, digest):
    result = results.get(name)
    if isinstance(result, Exception):
        fail(f"{name} request failed: {result}")
    if result is None:
        fail(f"{name} request did not produce a result")
    if result.status != 200:
        fail(f"{name} status={result.status}, want 200")
    if result.cache != want_cache:
        fail(f"{name} X-Mirror-Cache={result.cache!r}, want {want_cache!r}")
    actual = "sha256:" + hashlib.sha256(result.body).hexdigest()
    if actual != digest:
        fail(f"{name} body digest={actual}, want {digest}")
    return result


def main():
    fixture = get_json(f"{UPSTREAM}/__debug__/fixture")
    digest = fixture["layer_digest"]
    initial = get_json(f"{UPSTREAM}/__debug__/requests")
    initial_layer_gets = initial.get("layer_blob_gets", 0)
    if initial_layer_gets != 0:
        fail(f"fake registry is not fresh: layer_blob_gets={initial_layer_gets}, want 0")

    results = {}
    first_done = threading.Event()
    second_done = threading.Event()
    first = threading.Thread(target=fetch_blob, args=("regimux-a", REGIMUX_A, digest, results, first_done), daemon=True)
    first.start()
    wait_for_layer_get(initial_layer_gets, first_done)

    second = threading.Thread(target=fetch_blob, args=("regimux-b", REGIMUX_B, digest, results, second_done), daemon=True)
    second.start()
    join_thread(first, "regimux-a")
    join_thread(second, "regimux-b")

    first_result = require_result(results, "regimux-a", "miss", digest)
    second_result = require_result(results, "regimux-b", "hit", digest)
    final = get_json(f"{UPSTREAM}/__debug__/requests")
    if final.get("layer_blob_gets", 0) != 1:
        fail(f"upstream layer_blob_gets={final.get('layer_blob_gets')}, want 1")

    print(
        "multi-replica blob fill ok: "
        f"first_cache={first_result.cache} first_elapsed={first_result.elapsed:.3f}s "
        f"second_cache={second_result.cache} second_elapsed={second_result.elapsed:.3f}s "
        f"upstream_layer_blob_gets={final.get('layer_blob_gets')}"
    )


if __name__ == "__main__":
    main()
