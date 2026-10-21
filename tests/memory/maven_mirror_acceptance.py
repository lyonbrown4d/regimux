"""Exercise merged PR #8 against the final app image and offline HTTP fixtures."""
from __future__ import annotations

import hashlib
import json
from urllib.error import HTTPError
from urllib.request import Request, urlopen

FIXTURE = "http://fixture:8081"
APP = "http://regimux-maven8:8080"


def request(url: str, method: str = "GET") -> tuple[int, bytes, dict[str, str]]:
    try:
        with urlopen(Request(url, method=method), timeout=30) as response:
            return response.status, response.read(), dict(response.headers.items())
    except HTTPError as error:
        with error:
            return error.code, error.read(), dict(error.headers.items())


def fixture_target(wave: str) -> tuple[str, str, int]:
    status, body, _ = request(f"{FIXTURE}/__fixtures?wave={wave}")
    assert status == 200, (status, body)
    payload: object = json.loads(body)
    assert isinstance(payload, dict) and isinstance(payload.get("targets"), list)
    for target in payload["targets"]:
        if isinstance(target, dict) and target.get("name") == "maven-pom":
            path, digest, size = target.get("path"), target.get("sha256"), target.get("body_bytes")
            assert isinstance(path, str) and path.startswith("/maven/central/")
            assert isinstance(digest, str) and isinstance(size, int)
            return path.removeprefix("/maven/central"), digest, size
    raise AssertionError("Maven POM fixture is missing")


def counts() -> bytes:
    status, body, _ = request(f"{FIXTURE}/__stats")
    assert status == 200
    return body


def main() -> None:
    tail, digest, size = fixture_target("memory-maven8-get")
    url = f"{APP}/maven/fallback{tail}"
    status, body, _ = request(url)
    assert status == 200 and len(body) == size and hashlib.sha256(body).hexdigest() == digest
    before = counts()
    status, cached, _ = request(url)
    assert status == 200 and cached == body
    assert counts() == before, "cache hit contacted a Maven replica"

    head_tail, _, head_size = fixture_target("memory-maven8-head")
    status, body, headers = request(f"{APP}/maven/fallback{head_tail}", "HEAD")
    assert status == 200 and body == b""
    assert int(headers.get("Content-Length", "-1")) == head_size

    status, _, _ = request(f"{APP}/maven/missing{tail}")
    assert status == 404
    status, body, _ = request(f"{APP}/maven/unavailable{tail}")
    assert status == 503 and b"503" in body, (status, body)
    before = counts()
    status, _, _ = request(f"{APP}/maven/denied{tail}")
    assert status == 401
    after = counts()
    before_stats: object = json.loads(before)
    after_stats: object = json.loads(after)
    assert isinstance(before_stats, dict) and isinstance(after_stats, dict)
    upstream = "/maven" + tail
    assert before_stats["requests"].get(upstream) == after_stats["requests"].get(upstream), "401 contacted fallback"
    print(json.dumps({"maven_pr8": "passed", "cases": ["404-to-200", "cached-content", "HEAD", "all-404", "404-and-503", "terminal-401"]}))


if __name__ == "__main__":
    main()
