"""Aggregate per-case k6 summaries and optional Docker resource samples."""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any
from report_resources import resource_stats


ECOSYSTEMS = ("oci", "go", "npm", "pypi", "maven", "dist")
LATENCY_KEYS = {"avg": "avg", "p95": "p(95)", "p99": "p(99)", "max": "max"}
def mapping(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def number(value: Any) -> float | None:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    return float(value)


def integer(value: Any) -> int | None:
    parsed = number(value)
    return int(parsed) if parsed is not None and parsed.is_integer() else None


def metric_values(summary: dict[str, Any], name: str) -> dict[str, Any]:
    return mapping(mapping(mapping(summary.get("metrics")).get(name)).get("values"))


def metric_number(summary: dict[str, Any], name: str, key: str) -> float | None:
    return number(metric_values(summary, name).get(key))


def threshold_results(summary: dict[str, Any]) -> tuple[dict[str, dict[str, bool | None]], str]:
    results: dict[str, dict[str, bool | None]] = {}
    observed: list[bool | None] = []
    for name, raw in mapping(summary.get("metrics")).items():
        thresholds = mapping(mapping(raw).get("thresholds"))
        if not thresholds:
            continue
        results[name] = {}
        for expression, value in thresholds.items():
            status = mapping(value).get("ok")
            state = status if isinstance(status, bool) else None
            results[name][expression] = state
            observed.append(state)
    if not observed:
        return results, "unavailable"
    if any(value is False for value in observed):
        return results, "failed"
    if any(value is None for value in observed):
        return results, "unavailable"
    return results, "passed"


def case_metrics(data: dict[str, Any]) -> dict[str, Any]:
    configuration = mapping(data.get("configuration"))
    summary = mapping(data.get("summary"))
    iterations = metric_number(summary, "iterations", "count")
    successes = metric_values(summary, "successful_responses")
    http_failed = metric_values(summary, "http_req_failed")
    http_total = metric_number(summary, "http_reqs", "count")
    # For the Rate http_req_failed, k6's nonzero passes are failed requests.
    failed_count = integer(http_failed.get("passes"))
    checks = metric_values(summary, "checks")
    threshold_map, threshold_status = threshold_results(summary)
    latency: dict[str, dict[str, float | None]] = {}
    for ecosystem in ECOSYSTEMS:
        values = metric_values(summary, f"{ecosystem}_duration")
        latency[ecosystem] = {label: number(values.get(key)) for label, key in LATENCY_KEYS.items()}
    vus = number(configuration.get("concurrency"))
    return {
        "configuration": configuration,
        "configured_vus": int(vus) if vus is not None else None,
        "observed_max_vus": metric_number(summary, "vus_max", "max"),
        "completed_iterations": int(iterations) if iterations is not None else None,
        "successful_responses": {
            "count": integer(successes.get("count")),
            "rate_per_second": number(successes.get("rate")),
        },
        "http_requests": {
            "count": integer(http_total),
            "failed": failed_count,
            "failed_rate": number(http_failed.get("rate")),
        },
        "corrupt_response_failures": integer(metric_number(summary, "corrupt_responses", "count")),
        "check_failures": integer(checks.get("fails")),
        "upstream_gets": integer(metric_number(summary, "upstream_fetches", "count")),
        "thresholds": {"status": threshold_status, "results": threshold_map},
        "ecosystem_latency_ms": latency,
    }


def case_row(case_path: Path, prefix: str) -> dict[str, Any]:
    data = json.loads(case_path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError(f"{case_path}: k6 report must be a JSON object")
    if not isinstance(data.get("configuration"), dict) or not isinstance(data.get("summary"), dict):
        raise ValueError(f"{case_path}: expected outer configuration and summary objects")
    stem = case_path.name[:-5]
    resource_path = case_path.with_name(f"{stem}-resources.jsonl")
    final_path = case_path.with_name(f"{stem}-final-state.jsonl")
    return {
        "case": stem.removeprefix(prefix) or stem,
        "source": case_path.name,
        **case_metrics(data),
        "resources": resource_stats(resource_path, final_path),
        "resource_source": resource_path.name if resource_path.is_file() else None,
        "final_state_source": final_path.name if final_path.is_file() else None,
    }


def discover_cases(directory: Path, prefix: str) -> list[Path]:
    excluded = ("-resources.json", "-final-state.json")
    return sorted(path for path in directory.glob(f"{prefix}*.json")
                  if path.is_file() and not path.name.endswith(excluded))


def markdown_report(cases: list[dict[str, Any]]) -> str:
    lines = [
        "# Concurrency acceptance report", "",
        "Configured VUs, completed iterations, and measured successful response rate are separate quantities. VUs do not mean requests per second.", "",
        "## Case results", "",
        "| case | mode | configured VUs | iterations | successful responses | response rate/s | HTTP failures | corrupt responses | check failures | upstream GETs | thresholds |",
        "| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |",
    ]
    for case in cases:
        success = case["successful_responses"]
        http = case["http_requests"]
        config = case["configuration"]
        lines.append("| " + " | ".join((
            _cell(case["case"]), _cell(config.get("mode")), _fmt(case["configured_vus"], 0),
            _fmt(case["completed_iterations"], 0), _fmt(success.get("count"), 0),
            _fmt(success.get("rate_per_second"), 2), _fmt(http.get("failed"), 0),
            _fmt(case.get("corrupt_response_failures"), 0), _fmt(case.get("check_failures"), 0),
            _fmt(case.get("upstream_gets"), 0), case["thresholds"]["status"],
        )) + " |")
    lines.extend(["", "## Ecosystem latency", ""])
    for case in cases:
        lines.extend([f"### {case['case']}", "", "| ecosystem | avg ms | p95 ms | p99 ms | max ms |", "| --- | ---: | ---: | ---: | ---: |"])
        for ecosystem, values in case["ecosystem_latency_ms"].items():
            lines.append(f"| {ecosystem} | {_fmt(values['avg'], 2)} | {_fmt(values['p95'], 2)} | {_fmt(values['p99'], 2)} | {_fmt(values['max'], 2)} |")
        lines.append("")
    lines.extend(["## Container resources", ""])
    for case in cases:
        resources = case["resources"]
        lines.append(f"### {case['case']}")
        if not resources["available"] or not resources["containers"]:
            lines.extend(["", "Resource samples and final-state data: unavailable", ""])
            continue
        lines.extend(["", "| container | samples | peak CPU % | peak memory bytes | peak PIDs | OOM killed | final restarts | final exit code | final status |", "| --- | ---: | ---: | ---: | ---: | --- | ---: | ---: | --- |"])
        for name, state in resources["containers"].items():
            oom = "unavailable" if state["oom_killed"] is None else str(state["oom_killed"]).lower()
            lines.append(f"| {_cell(name)} | {_fmt(state['sample_count'], 0)} | {_fmt(state['peak_cpu_percent'], 2)} | {_fmt(state['peak_memory_bytes'], 0)} | {_fmt(state['peak_pids'], 0)} | {oom} | {_fmt(state['final_restart_count'], 0)} | {_fmt(state['final_exit_code'], 0)} | {_cell(state['final_status'])} |")
        lines.append("")
    return "\n".join(lines).rstrip() + "\n"


def _cell(value: Any) -> str:
    return "unavailable" if value is None else str(value).replace("|", "\\|").replace("\n", " ")


def _fmt(value: Any, digits: int) -> str:
    parsed = number(value)
    return "unavailable" if parsed is None else f"{parsed:.{digits}f}"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", type=Path, required=True, help="directory containing per-case k6 JSON")
    parser.add_argument("--prefix", required=True, help="case filename prefix; resource/final-state JSONL files are excluded")
    parser.add_argument("--markdown", "--markdown-output", dest="markdown", type=Path, required=True)
    parser.add_argument("--json", "--json-output", dest="json_output", type=Path, required=True)
    args = parser.parse_args()
    if not args.directory.is_dir():
        parser.error(f"report directory does not exist: {args.directory}")
    paths = discover_cases(args.directory, args.prefix)
    if not paths:
        parser.error(f"no per-case k6 JSON files matching {args.prefix}*.json in {args.directory}")
    cases = [case_row(path, args.prefix) for path in paths]
    report = {"directory": str(args.directory), "prefix": args.prefix, "cases": cases}
    args.json_output.parent.mkdir(parents=True, exist_ok=True)
    args.markdown.parent.mkdir(parents=True, exist_ok=True)
    args.json_output.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    args.markdown.write_text(markdown_report(cases), encoding="utf-8")
    print(f"wrote {len(cases)} cases to {args.json_output} and {args.markdown}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
