"""Capture private HTTP health and goroutine counts before/after a load matrix."""
from __future__ import annotations

import argparse
import json
import re
import time
from urllib.request import urlopen


def sample(base: str) -> dict[str, str | int]:
    with urlopen(base + "/livez", timeout=10) as response:
        status = response.status
    with urlopen(base + "/debug/pprof/goroutine?debug=1", timeout=10) as response:
        profile = response.read().decode()
    match = re.search(r"goroutine profile: total (\d+)", profile)
    if match is None:
        raise ValueError(f"unrecognized goroutine profile from {base}")
    return {"base": base, "health_status": status, "goroutines": int(match.group(1))}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--peer", action="store_true")
    parser.add_argument("--samples", type=int, default=1, choices=range(1, 7))
    args = parser.parse_args()
    bases = ["http://regimux:8080"]
    if args.peer:
        bases.append("http://regimux-peer:8080")
    for index in range(args.samples):
        if index:
            time.sleep(5)
        print(json.dumps({"time": time.time(), "services": [sample(base) for base in bases]}), flush=True)


if __name__ == "__main__":
    main()
