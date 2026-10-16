"""Parse optional Docker resource samples and final container state."""
from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any


BYTE_UNITS = {
    "b": 1, "kb": 1000, "mb": 1000**2, "gb": 1000**3, "tb": 1000**4,
    "kib": 1024, "mib": 1024**2, "gib": 1024**3, "tib": 1024**4,
}


def parse_percent(value: Any) -> float | None:
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return float(value)
    if not isinstance(value, str):
        return None
    match = re.fullmatch(r"\s*([0-9]+(?:\.[0-9]+)?)\s*%\s*", value)
    return float(match.group(1)) if match else None


def parse_bytes(value: Any) -> int | None:
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return int(value)
    if not isinstance(value, str):
        return None
    first = value.split("/", 1)[0].strip()
    match = re.fullmatch(r"([0-9]+(?:\.[0-9]+)?)\s*([A-Za-z]+)?", first)
    if not match:
        return None
    unit = (match.group(2) or "b").lower()
    multiplier = BYTE_UNITS.get(unit)
    return int(float(match.group(1)) * multiplier) if multiplier is not None else None


def parse_pids(value: Any) -> int | None:
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        return None
    return parsed if parsed >= 0 else None


def resource_stats(path: Path | None, final_path: Path | None) -> dict[str, Any]:
    containers: dict[str, dict[str, Any]] = {}
    for row in read_jsonl(path):
        stats = _mapping(row.get("stats", row))
        name = str(stats.get("Name") or row.get("name") or "").strip().lstrip("/")
        if not name:
            continue
        state = containers.setdefault(name, _new_container_state())
        state["sample_count"] += 1
        _maximize(state, "peak_cpu_percent", parse_percent(stats.get("CPUPerc")))
        _maximize(state, "peak_memory_bytes", parse_bytes(stats.get("MemUsage")))
        _maximize(state, "peak_pids", parse_pids(stats.get("PIDs")))
    for row in read_jsonl(final_path):
        name = _find_name(row) or "unidentified"
        state = containers.setdefault(name, _new_container_state())
        parsed_oom = _parse_bool(_find_value(row, {"oomkilled", "oom_killed"}))
        if parsed_oom is not None:
            state["oom_killed"] = parsed_oom
        restarts = parse_pids(_find_value(row, {"restartcount", "restart_count", "restarts"}))
        if restarts is not None:
            state["final_restart_count"] = restarts
        exit_code = parse_pids(_find_value(row, {"exitcode", "exit_code"}))
        if exit_code is not None:
            state["final_exit_code"] = exit_code
        status = _find_value(row, {"status"})
        if status is not None:
            state["final_status"] = str(status)
    return {
        "available": bool(path and path.exists()) or bool(final_path and final_path.exists()),
        "containers": dict(sorted(containers.items())) if containers else None,
    }


def _new_container_state() -> dict[str, Any]:
    return {
        "sample_count": 0, "peak_cpu_percent": None, "peak_memory_bytes": None,
        "peak_pids": None, "oom_killed": None, "final_restart_count": None,
        "final_exit_code": None, "final_status": None,
    }


def _maximize(target: dict[str, Any], key: str, value: float | int | None) -> None:
    if value is not None and (target[key] is None or value > target[key]):
        target[key] = value


def _mapping(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _find_value(value: Any, aliases: set[str]) -> Any:
    if isinstance(value, dict):
        for key, nested in value.items():
            if str(key).lower() in aliases:
                return nested
        for nested in value.values():
            found = _find_value(nested, aliases)
            if found is not None:
                return found
    elif isinstance(value, list):
        for nested in value:
            found = _find_value(nested, aliases)
            if found is not None:
                return found
    return None


def _find_name(value: Any) -> str | None:
    found = _find_value(value, {"name", "container_name"})
    return str(found).strip().lstrip("/") if found else None


def _parse_bool(value: Any) -> bool | None:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        if value.lower() in {"true", "1", "yes"}:
            return True
        if value.lower() in {"false", "0", "no"}:
            return False
    return None


def read_jsonl(path: Path | None) -> list[dict[str, Any]]:
    if path is None or not path.is_file():
        return []
    rows: list[dict[str, Any]] = []
    with path.open(encoding="utf-8") as stream:
        for line_number, line in enumerate(stream, 1):
            if not line.strip():
                continue
            value = json.loads(line)
            if isinstance(value, dict):
                rows.append(value)
            elif isinstance(value, list) and all(isinstance(item, dict) for item in value):
                rows.extend(value)
            else:
                raise ValueError(f"{path}:{line_number}: JSONL row must be an object or list of objects")
    return rows
