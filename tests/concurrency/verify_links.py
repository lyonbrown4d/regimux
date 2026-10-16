"""Follow rewritten npm/PyPI metadata links through the isolated proxy."""
from __future__ import annotations

import argparse
import hashlib
import json
import re
from html.parser import HTMLParser
from urllib.parse import urljoin, urlsplit
from urllib.request import urlopen


class Links(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.hrefs: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag == "a":
            self.hrefs.extend(value for key, value in attrs if key == "href" and value)


def read(url: str) -> bytes:
    with urlopen(url, timeout=45) as response:
        if response.status != 200:
            raise ValueError(f"unexpected response: {response.status} for {url}")
        return response.read()


def follow(base: str, link: str, expected_hash: str) -> None:
    resolved = urljoin(base, link)
    if urlsplit(resolved).netloc != urlsplit(base).netloc:
        raise ValueError(f"rewritten link bypasses proxy: {resolved}")
    digest = hashlib.sha256(read(resolved)).hexdigest()
    if digest != expected_hash:
        raise ValueError(f"download hash mismatch: {resolved}: {digest}")
    print(f"PASS followed exact package bytes: {resolved}")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--wave", required=True)
    parser.add_argument("--base", default="http://regimux:8080")
    args = parser.parse_args()
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,47}", args.wave):
        parser.error("invalid wave")
    if args.base not in {"http://regimux:8080", "http://regimux-peer:8080"}:
        parser.error("base must refer to an isolated Compose service")
    manifest = json.loads(read(f"http://fixture:8081/__fixtures?wave={args.wave}"))
    targets = {target["name"]: target for target in manifest["targets"]}
    metadata = json.loads(read(args.base + targets["npm-metadata"]["path"]))
    link = metadata["versions"]["1.0.0"]["dist"]["tarball"]
    follow(args.base, link, targets["npm-tarball"]["sha256"])
    index = Links()
    index.feed(read(args.base + targets["pypi-simple"]["path"]).decode())
    if len(index.hrefs) != 1:
        raise ValueError(f"expected exactly one fixture wheel link: {index.hrefs}")
    follow(args.base, index.hrefs[0], targets["pypi-wheel"]["sha256"])


if __name__ == "__main__":
    main()
