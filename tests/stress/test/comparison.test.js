import assert from "node:assert/strict";
import test from "node:test";

import {
  buildComparison,
  markdownComparison,
  normalizeReport,
} from "../k6/comparison.js";

const resources = {
  service: "regimux",
  sample_count: 3,
  sampling_interval_ms: 1000,
  cpu_percent: { avg: 10, p95: 20, peak: 30 },
  memory_bytes: { avg: 1048576, p95: 2097152, peak: 3145728 },
  memory_percent: { avg: 1, p95: 2, peak: 3 },
  pids: { avg: 4, p95: 5, peak: 6 },
  network_io: {
    rx_bytes: { start: 100, end: 500, delta: 400 },
    tx_bytes: { start: 200, end: 800, delta: 600 },
  },
  block_io: {
    read_bytes: { start: 10, end: 30, delta: 20 },
    write_bytes: { start: 40, end: 70, delta: 30 },
  },
  restart_count: { start: 0, end: 1, delta: 1 },
  oom_killed: { observed: true, final: false },
};

test("normalizeReport retains resource metrics alongside existing scenario metrics", () => {
  const normalized = normalizeReport(
    {
      schema_version: 1,
      profile: "load",
      metadata_store: "sqlite",
      base_url: "http://regimux:8080",
      generated_at: "2026-07-16T00:00:00.000Z",
      overall: { requests: 10, duration_ms: { p95: 2 } },
      scenarios: [{ name: "hot", request_rate: 7 }],
      endpoints: [{ name: "endpoint", duration_ms: { p95: 12.34 } }],
      resources,
    },
    { store: "sqlite", path: "/stress/reports/sqlite.json" },
  );

  assert.equal(normalized.scenarios[0].request_rate, 7);
  assert.equal(normalized.resources.network_io.rx_bytes.delta, 400);
});

test("comparison JSON and Markdown include resources without dropping endpoint p95", () => {
  const items = [
    {
      store: "sqlite",
      source: "/stress/reports/sqlite.json",
      profile: "load",
      base_url: "http://regimux:8080",
      generated_at: "2026-07-16T00:00:00.000Z",
      overall: {
        requests: 10,
        request_rate: 2,
        failed_rate: 0,
        duration_ms: { avg: 5, p95: 6, p99: 7 },
        data_received_bytes: 1024,
      },
      scenarios: [{ name: "hot", request_rate: 7, duration_ms: { p95: 8 } }],
      endpoints: [{ name: "endpoint", request_rate: 4, duration_ms: { p95: 12.34 } }],
      resources,
    },
  ];

  const comparison = buildComparison(
    items,
    "comparison",
    "2026-07-16T01:00:00.000Z",
  );
  const markdown = markdownComparison(comparison);

  assert.equal(comparison.scenarios[0].values.sqlite.request_rate, 7);
  assert.equal(comparison.resources.sqlite.cpu_percent.peak, 30);
  assert.match(markdown, /\| endpoint \| 12\.34 ms \|/);
  assert.match(markdown, /\| sqlite \| CPU \| 10\.00% \| 20\.00% \| 30\.00% \|/);
  assert.match(markdown, /\| sqlite \| 400 B \| 600 B \| 20 B \| 30 B \| 1 \| yes \| no \|/);
});