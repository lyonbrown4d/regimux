import { readFile, writeFile } from "node:fs/promises";

const byteMultipliers = new Map([
  ["", 1],
  ["b", 1],
  ["kb", 1_000],
  ["mb", 1_000_000],
  ["gb", 1_000_000_000],
  ["tb", 1_000_000_000_000],
  ["kib", 2 ** 10],
  ["mib", 2 ** 20],
  ["gib", 2 ** 30],
  ["tib", 2 ** 40],
]);

export function parseDockerStats(data) {
  const lines = String(data)
    .trim()
    .split(/\r?\n/)
    .filter(Boolean);
  if (lines.length !== 1) {
    throw new Error("docker stats must contain exactly one container");
  }

  const raw = JSON.parse(lines[0]);
  const [memoryBytes] = parseBytePair(raw.MemUsage);
  const [networkRXBytes, networkTXBytes] = parseBytePair(raw.NetIO);
  const [blockReadBytes, blockWriteBytes] = parseBytePair(raw.BlockIO);

  return {
    cpuPercent: parsePercent(raw.CPUPerc),
    memoryBytes,
    memoryPercent: parsePercent(raw.MemPerc),
    pids: parseUnsignedInteger(raw.PIDs, "PIDs"),
    networkRXBytes,
    networkTXBytes,
    blockReadBytes,
    blockWriteBytes,
    restartCount: 0,
    oomKilled: false,
  };
}

export function parseDockerInspect(data) {
  const decoded = JSON.parse(String(data));
  const raw = Array.isArray(decoded) ? decoded[0] : decoded;
  if (!raw || typeof raw !== "object") {
    throw new Error("docker inspect must contain one container");
  }

  return {
    restartCount: parseUnsignedInteger(raw.RestartCount, "restart count"),
    oomKilled: Boolean(raw.State && raw.State.OOMKilled),
  };
}

export function aggregateResources(samples, intervalMs, service) {
  if (!Array.isArray(samples) || samples.length === 0) {
    throw new Error("resource samples are empty");
  }
  if (!Number.isFinite(intervalMs) || intervalMs <= 0) {
    throw new Error("sampling interval must be positive");
  }

  return {
    service,
    sample_count: samples.length,
    sampling_interval_ms: intervalMs,
    cpu_percent: summarizeGauge(samples.map((sample) => sample.cpuPercent)),
    memory_bytes: summarizeGauge(samples.map((sample) => sample.memoryBytes)),
    memory_percent: summarizeGauge(samples.map((sample) => sample.memoryPercent)),
    pids: summarizeGauge(samples.map((sample) => sample.pids)),
    network_io: {
      rx_bytes: summarizeCounter(samples.map((sample) => sample.networkRXBytes)),
      tx_bytes: summarizeCounter(samples.map((sample) => sample.networkTXBytes)),
    },
    block_io: {
      read_bytes: summarizeCounter(samples.map((sample) => sample.blockReadBytes)),
      write_bytes: summarizeCounter(samples.map((sample) => sample.blockWriteBytes)),
    },
    restart_count: summarizeCounter(samples.map((sample) => sample.restartCount)),
    oom_killed: {
      observed: samples.some((sample) => sample.oomKilled),
      final: samples.at(-1).oomKilled,
    },
  };
}

export async function augmentReport(jsonPath, markdownPath, resources) {
  const [jsonData, markdownData] = await Promise.all([
    readFile(jsonPath, "utf8"),
    readFile(markdownPath, "utf8"),
  ]);
  const report = JSON.parse(jsonData);
  if (!report || typeof report !== "object" || Array.isArray(report)) {
    throw new Error("k6 JSON report is not an object");
  }
  if (Object.hasOwn(report, "resources")) {
    throw new Error("k6 JSON report already contains resources");
  }
  if (markdownData.includes("## RegiMux Container Resources")) {
    throw new Error("k6 Markdown report already contains resources");
  }

  report.resources = resources;
  const markdown = markdownData.replace(/[\r\n]+$/, "") + "\n\n" + resourceMarkdown(resources);
  await Promise.all([
    writeFile(jsonPath, JSON.stringify(report, null, 2) + "\n"),
    writeFile(markdownPath, markdown),
  ]);
}

export function resourceMarkdown(resources) {
  const lines = [
    "## RegiMux Container Resources",
    "",
    "- service: " + resources.service,
    "- samples: " + resources.sample_count,
    "- sampling_interval_ms: " + resources.sampling_interval_ms,
    "",
    "### Gauges",
    "",
    "| metric | avg | p95 | peak |",
    "| --- | ---: | ---: | ---: |",
    gaugeRow("CPU", resources.cpu_percent, formatPercent),
    gaugeRow("memory usage", resources.memory_bytes, formatBytes),
    gaugeRow("memory utilization", resources.memory_percent, formatPercent),
    gaugeRow("PIDs", resources.pids, formatPID),
    "",
    "### Cumulative Counters",
    "",
    "| metric | start | end | delta |",
    "| --- | ---: | ---: | ---: |",
    counterRow("network received", resources.network_io.rx_bytes, formatBytes),
    counterRow("network sent", resources.network_io.tx_bytes, formatBytes),
    counterRow("block read", resources.block_io.read_bytes, formatBytes),
    counterRow("block written", resources.block_io.write_bytes, formatBytes),
    counterRow("restart count", resources.restart_count, String),
    "",
    "### Status",
    "",
    "| status | value |",
    "| --- | ---: |",
    "| OOM killed observed | " + formatBoolean(resources.oom_killed.observed) + " |",
    "| OOM killed final | " + formatBoolean(resources.oom_killed.final) + " |",
    "",
  ];
  return lines.join("\n");
}

function parsePercent(value) {
  const parsed = Number(String(value).trim().replace(/%$/, ""));
  if (!Number.isFinite(parsed) || parsed < 0) {
    throw new Error("invalid percentage: " + value);
  }
  return parsed;
}

function parseBytePair(value) {
  const parts = String(value).split("/");
  if (parts.length !== 2) {
    throw new Error("expected two byte values: " + value);
  }
  return parts.map(parseBytes);
}

function parseBytes(value) {
  const match = String(value)
    .trim()
    .match(/^(\d+(?:\.\d*)?|\.\d+)\s*([a-zA-Z]*)$/);
  if (!match) {
    throw new Error("invalid byte value: " + value);
  }

  const number = Number(match[1]);
  const multiplier = byteMultipliers.get(match[2].toLowerCase());
  if (!Number.isFinite(number) || number < 0 || multiplier === undefined) {
    throw new Error("invalid byte value: " + value);
  }
  return Math.round(number * multiplier);
}

function parseUnsignedInteger(value, label) {
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < 0) {
    throw new Error("invalid " + label + ": " + value);
  }
  return parsed;
}

function summarizeGauge(values) {
  const sorted = [...values].sort((left, right) => left - right);
  const total = values.reduce((sum, value) => sum + value, 0);
  const p95Index = Math.ceil(sorted.length * 0.95) - 1;
  return {
    avg: total / values.length,
    p95: sorted[p95Index],
    peak: sorted.at(-1),
  };
}

function summarizeCounter(values) {
  let delta = 0;
  for (let index = 1; index < values.length; index += 1) {
    const previous = values[index - 1];
    const current = values[index];
    delta += current >= previous ? current - previous : current;
  }
  return {
    start: values[0],
    end: values.at(-1),
    delta,
  };
}

function gaugeRow(name, summary, formatter) {
  return "| " + name + " | " + formatter(summary.avg) + " | " +
    formatter(summary.p95) + " | " + formatter(summary.peak) + " |";
}

function counterRow(name, summary, formatter) {
  return "| " + name + " | " + formatter(summary.start) + " | " +
    formatter(summary.end) + " | " + formatter(summary.delta) + " |";
}

function formatPercent(value) {
  return value.toFixed(2) + "%";
}

function formatPID(value) {
  return Number.isInteger(value) ? String(value) : value.toFixed(2);
}

function formatBytes(value) {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let size = Number(value);
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return size.toFixed(unit === 0 ? 0 : 2) + " " + units[unit];
}

function formatBoolean(value) {
  return value ? "yes" : "no";
}