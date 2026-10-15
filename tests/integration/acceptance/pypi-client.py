#!/usr/bin/env python3
"""Run pip and uv acceptance clients against a RegiMux PyPI alias."""

from __future__ import annotations

import importlib.util
import json
import os
import shutil
import subprocess
import sys
import tempfile
import urllib.parse
from pathlib import Path
from typing import NoReturn

from pypi_acceptance_http import six_wheel_url, verify_cached_ranges

BASE_URL = os.environ.get("REGIMUX_BASE_URL", "http://regimux:8080").rstrip("/")
ALIAS = os.environ.get("REGIMUX_PYPI_ALIAS", "default")
COMMAND_TIMEOUT_SECONDS = 600
PACKAGE_VERSIONS = ("six==1.17.0", "requests==2.32.3")
SIX_WHEEL = "six-1.17.0-py2.py3-none-any.whl"
REQUESTS_WHEEL = "requests-2.32.3-py3-none-any.whl"


def fail(message: str) -> NoReturn:
    print(f"FAIL: {message}", file=sys.stderr, flush=True)
    raise SystemExit(1)


def checked_url(value: str) -> urllib.parse.SplitResult:
    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname or parsed.username or parsed.password:
        fail("REGIMUX_BASE_URL must be an HTTP(S) origin without credentials")
    if parsed.path not in {"", "/"} or parsed.query or parsed.fragment:
        fail("REGIMUX_BASE_URL must be an origin without a path, query, or fragment")
    allowed_alias_chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
    if not ALIAS or any(ch not in allowed_alias_chars for ch in ALIAS):
        fail("REGIMUX_PYPI_ALIAS contains unsupported characters")
    return parsed


def run_command(label: str, command: list[str], cwd: Path, env: dict[str, str]) -> None:
    print(f"[pypi-acceptance] {label}", flush=True)
    try:
        result = subprocess.run(
            command,
            cwd=cwd,
            env=env,
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=COMMAND_TIMEOUT_SECONDS,
        )
    except subprocess.TimeoutExpired as exc:
        output = exc.stdout or ""
        if isinstance(output, bytes):
            output = output.decode("utf-8", errors="replace")
        fail(f"{label} exceeded {COMMAND_TIMEOUT_SECONDS}s\n{output[-8000:]}")
    except OSError as exc:
        fail(f"could not start {label}: {exc}")
    if result.returncode != 0:
        fail(f"{label} exited {result.returncode}\n{result.stdout[-8000:]}")


def client_environment(uv_cache_dir: Path) -> dict[str, str]:
    env = os.environ.copy()
    for key in (
        "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy",
        "PIP_INDEX_URL", "PIP_EXTRA_INDEX_URL", "PIP_FIND_LINKS", "PIP_NO_INDEX", "PIP_TRUSTED_HOST",
        "UV_INDEX", "UV_INDEX_URL", "UV_EXTRA_INDEX_URL", "UV_DEFAULT_INDEX", "UV_NO_INDEX", "UV_PYTHON",
    ):
        env.pop(key, None)
    env.update(
        {
            "PIP_CONFIG_FILE": os.devnull,
            "PIP_DISABLE_PIP_VERSION_CHECK": "1",
            "UV_NO_CONFIG": "1",
            "UV_CACHE_DIR": str(uv_cache_dir),
        }
    )
    return env


def pip_download(
    uv: str,
    python: str,
    index_url: str,
    directory: Path,
    cache_dir: Path,
    env: dict[str, str],
    host: str,
) -> None:
    directory.mkdir(parents=True)
    cache_dir.mkdir(parents=True)
    command = [
        uv, "run", "--no-project", "--python", python, "--", python, "-m", "pip", "download",
        "--index-url", index_url, "--only-binary=:all:", "--timeout", "45", "--retries", "2",
        "--cache-dir", str(cache_dir), "--dest", str(directory), *PACKAGE_VERSIONS,
    ]
    if urllib.parse.urlsplit(index_url).scheme == "http":
        command.extend(("--trusted-host", host))
    run_command(f"pip download via {index_url}", command, directory, env)


def require_downloaded_packages(directory: Path, label: str) -> Path:
    six_wheel = directory / SIX_WHEEL
    requests_wheel = directory / REQUESTS_WHEEL
    for wheel in (six_wheel, requests_wheel):
        if not wheel.is_file():
            fail(f"{label} did not download expected public wheel {wheel.name}")
    return six_wheel


def uv_project(uv: str, python: str, index_url: str, project: Path, cache_dir: Path, env: dict[str, str]) -> None:
    project.mkdir(parents=True)
    (project / "pyproject.toml").write_text(
        """[project]
name = "regimux-pypi-acceptance"
version = "0.1.0"
requires-python = ">=3.9"
dependencies = ["requests==2.32.3", "six==1.17.0"]
""",
        encoding="utf-8",
    )
    uv_env = env.copy()
    uv_env["UV_CACHE_DIR"] = str(cache_dir)
    run_command(
        "uv lock through RegiMux",
        [uv, "lock", "--no-progress", "--python", python, "--default-index", index_url],
        project,
        uv_env,
    )
    run_command(
        "verify uv lock is current",
        [uv, "lock", "--check", "--no-progress", "--python", python, "--default-index", index_url],
        project,
        uv_env,
    )
    run_command(
        "uv sync through RegiMux",
        [uv, "sync", "--frozen", "--no-install-project", "--no-progress", "--python", python, "--default-index", index_url],
        project,
        uv_env,
    )
    venv_python = project / ".venv" / "bin" / "python"
    if not venv_python.is_file():
        fail("uv sync completed but did not create .venv/bin/python")
    run_command(
        "verify uv-installed packages",
        [
            uv, "run", "--frozen", "--python", python, "--", "python", "-c",
            "import requests, six; assert requests.__version__ == '2.32.3'; assert six.__version__ == '1.17.0'",
        ],
        project,
        uv_env,
    )


def write_wheel_report(url: str, size: int, expected_hash: str) -> None:
    report_dir = Path(os.environ.get("REPORT_DIR", "/reports"))
    report_path = report_dir / "pypi-wheel.json"
    payload = {
        "ecosystem": "pypi",
        "alias": ALIAS,
        "filename": SIX_WHEEL,
        "url": url,
        "size": size,
        "sha256": expected_hash,
    }
    try:
        report_dir.mkdir(parents=True, exist_ok=True)
        report_path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    except OSError as exc:
        fail(f"could not write wheel report to {report_path}: {exc}")


def main() -> None:
    base = checked_url(BASE_URL)
    index_url = f"{BASE_URL}/pypi/{urllib.parse.quote(ALIAS, safe='')}/simple/"
    python = sys.executable
    uv = shutil.which("uv")
    if not uv:
        fail("uv was not found in PATH; run this script in the Python/uv acceptance container")
    if importlib.util.find_spec("pip") is None:
        fail("pip is not installed in the uv-managed acceptance Python")
    host = base.hostname or ""

    with tempfile.TemporaryDirectory(prefix="regimux-pypi-acceptance-") as temp_root:
        root = Path(temp_root)
        pip_env = client_environment(root / "pip-env-cache")
        first_dir, first_cache = root / "pip-first", root / "pip-cache-first"
        pip_download(uv, python, index_url, first_dir, first_cache, pip_env, host)
        first_wheel = require_downloaded_packages(first_dir, "initial pip download")

        uv_project(uv, python, index_url, root / "uv-project", root / "uv-cache", pip_env)

        second_dir, second_cache = root / "pip-second", root / "pip-cache-second"
        pip_download(uv, python, index_url, second_dir, second_cache, pip_env, host)
        second_wheel = require_downloaded_packages(second_dir, "empty-cache pip download")

        wheel_url, expected_hash, report_url = six_wheel_url(index_url, base, ALIAS, SIX_WHEEL)
        size = verify_cached_ranges(wheel_url, expected_hash, first_wheel.read_bytes(), second_wheel.read_bytes())
        write_wheel_report(report_url, size, expected_hash)

    print("PyPI acceptance passed: pip/uv downloads, empty client cache, SHA-256, HEAD, Range, If-Range, 416, intact cache", flush=True)


if __name__ == "__main__":
    main()
