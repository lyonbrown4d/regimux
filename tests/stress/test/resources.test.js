import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  aggregateResources,
  augmentReport,
  parseDockerInspect,
  parseDockerStats,
} from "../resources.js";

const samples = [
  {
    cpuPercent: 10,
    memoryBytes: 100,
    memoryPercent: 1,
    pids: 2,
    networkRXBytes: 100,
    networkTXBytes: 50,
    blockReadBytes: 1000,
    blockWriteBytes: 500,
    restartCount: 2,
    oomKilled: false,
  },
  {
    cpuPercent: 30,
    memoryBytes: 300,
    memoryPercent: 3,
    pids: 4,
    networkRXBytes: 160,
    networkTXBytes: 90,
    blockReadBytes: 1300,
    blockWriteBytes: 700,
    restartCount: 2,
    oomKilled: true,
  },
  {
    cpuPercent: 20,
    memoryBytes: 200,
    memoryPercent: 2,
    pids: 3,
    networkRXBytes: 20,
    networkTXBytes: 10,
    blockReadBytes: 100,
    blockWriteBytes: 50,
    restartCount: 3,
    oomKilled: false,
  },
];

test("parseDockerStats handles Docker decimal and binary units", () => {
  const parsed = parseDockerStats(JSON.stringify({
    CPUPerc: "12.50%",
    MemUsage: "128MiB / 2GiB",
    MemPerc: "6.25%",
    NetIO: "1.5MB / 2KiB",
    BlockIO: "3GB / 4.5MB",
    PIDs: "17",
  }));

  assert.deepEqual(parsed, {
    cpuPercent: 12.5,
    memoryBytes: 128 * 1024 * 1024,
    memoryPercent: 6.25,
    pids: 17,
    networkRXBytes: 1_500_000,
    networkTXBytes: 2 * 1024,
    blockReadBytes: 3_000_000_000,
    blockWriteBytes: 4_500_000,
    restartCount: 0,
    oomKilled: false,
  });
});

test("parseDockerInspect reads restart and OOM state", () => {
  const parsed = parseDockerInspect(JSON.stringify([{
    RestartCount: 3,
    State: { OOMKilled: true },
  }]));

  assert.deepEqual(parsed, { restartCount: 3, oomKilled: true });
});

test("aggregateResources uses nearest-rank p95 and reset-aware deltas", () => {
  const resources = aggregateResources(samples, 1500, "regimux");

  assert.deepEqual(resources, {
    service: "regimux",
    sample_count: 3,
    sampling_interval_ms: 1500,
    cpu_percent: { avg: 20, p95: 30, peak: 30 },
    memory_bytes: { avg: 200, p95: 300, peak: 300 },
    memory_percent: { avg: 2, p95: 3, peak: 3 },
    pids: { avg: 3, p95: 4, peak: 4 },
    network_io: {
      rx_bytes: { start: 100, end: 20, delta: 80 },
      tx_bytes: { start: 50, end: 10, delta: 50 },
    },
    block_io: {
      read_bytes: { start: 1000, end: 100, delta: 400 },
      write_bytes: { start: 500, end: 50, delta: 250 },
    },
    restart_count: { start: 2, end: 3, delta: 1 },
    oom_killed: { observed: true, final: false },
  });
});

test("augmentReport preserves k6 metrics and writes deterministic resources", async (t) => {
  const first = await writeFixtureReport(t);
  const second = await writeFixtureReport(t);
  const resources = aggregateResources(samples, 1000, "regimux");

  await augmentReport(first.json, first.markdown, resources);
  await augmentReport(second.json, second.markdown, resources);

  const [firstJSON, secondJSON, firstMarkdown, secondMarkdown] = await Promise.all([
    readFile(first.json, "utf8"),
    readFile(second.json, "utf8"),
    readFile(first.markdown, "utf8"),
    readFile(second.markdown, "utf8"),
  ]);
  const report = JSON.parse(firstJSON);

  assert.equal(report.overall.requests, 42);
  assert.equal(report.scenarios[0].request_rate, 7.5);
  assert.equal(report.resources.network_io.rx_bytes.delta, 80);
  assert.equal(firstJSON, secondJSON);
  assert.equal(firstMarkdown, secondMarkdown);
  assert.match(firstMarkdown, /existing scenario content/);
  assert.match(firstMarkdown, /\| CPU \| 20\.00% \| 30\.00% \| 30\.00% \|/);
  assert.match(firstMarkdown, /\| restart count \| 2 \| 3 \| 1 \|/);
  assert.match(firstMarkdown, /\| OOM killed observed \| yes \|/);
});

test("aggregateResources rejects empty samples", () => {
  assert.throws(
    () => aggregateResources([], 1000, "regimux"),
    /resource samples are empty/,
  );
});

async function writeFixtureReport(t) {
  const directory = await mkdtemp(join(tmpdir(), "regimux-stress-"));
  t.after(() => rm(directory, { recursive: true, force: true }));

  const json = join(directory, "report.json");
  const markdown = join(directory, "report.md");
  await Promise.all([
    writeFile(json, JSON.stringify({
      schema_version: 1,
      overall: { requests: 42 },
      scenarios: [{ name: "hot", request_rate: 7.5 }],
    }, null, 2) + "\n"),
    writeFile(
      markdown,
      "# RegiMux k6 Stress Report\n\n## Scenarios\n\nexisting scenario content\n",
    ),
  ]);
  return { json, markdown };
}