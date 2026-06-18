const REPORT_DIR = __ENV.REGIMUX_K6_REPORT_DIR || "/stress/reports";
const REPORT_NAME = cleanReportName(__ENV.REGIMUX_K6_COMPARE_REPORT_NAME || "regimux-stress-load-comparison");
const REPORT_SPECS = parseReportSpecs(__ENV.REGIMUX_K6_COMPARE_REPORTS || [
  "sqlite=/stress/reports/regimux-stress-sqlite-load.json",
  "mysql=/stress/reports/regimux-stress-mysql-load.json",
  "postgres=/stress/reports/regimux-stress-postgres-load.json",
].join(","));

const reports = REPORT_SPECS.map((spec) => loadReport(spec));

export const options = {
  scenarios: {
    compare_reports: {
      executor: "shared-iterations",
      vus: 1,
      iterations: 1,
      maxDuration: "1s",
    },
  },
};

export default function () {
}

export function handleSummary() {
  const comparison = buildComparison(reports);
  const markdown = markdownComparison(comparison);
  const json = `${JSON.stringify(comparison, null, 2)}\n`;
  const basePath = `${REPORT_DIR}/${REPORT_NAME}`;
  return {
    stdout: markdown,
    [`${basePath}.md`]: markdown,
    [`${basePath}.json`]: json,
  };
}

function parseReportSpecs(value) {
  return String(value || "").split(",").map((item) => item.trim()).filter(Boolean).map((item) => {
    const parts = item.split("=");
    if (parts.length !== 2 || !parts[0] || !parts[1]) {
      throw new Error(`invalid REGIMUX_K6_COMPARE_REPORTS item: ${item}`);
    }
    return { store: cleanReportName(parts[0]), path: parts[1] };
  });
}

function loadReport(spec) {
  const parsed = JSON.parse(open(spec.path));
  if (parsed.schema_version) {
    return {
      store: spec.store || parsed.metadata_store,
      source: spec.path,
      profile: parsed.profile,
      base_url: parsed.base_url,
      generated_at: parsed.generated_at,
      overall: parsed.overall,
      scenarios: parsed.scenarios || [],
      endpoints: parsed.endpoints || [],
    };
  }

  return {
    store: spec.store,
    source: spec.path,
    profile: "unknown",
    base_url: "unknown",
    generated_at: null,
    overall: legacyMetricRow(parsed, ""),
    scenarios: legacyRows(parsed, "scenario"),
    endpoints: legacyRows(parsed, "endpoint"),
  };
}

function buildComparison(items) {
  return {
    schema_version: 1,
    generated_at: new Date().toISOString(),
    report_name: REPORT_NAME,
    profile: commonValue(items.map((item) => item.profile)) || "mixed",
    base_url: commonValue(items.map((item) => item.base_url)) || "mixed",
    stores: items,
    scenarios: unionRows(items, "scenarios"),
    endpoints: unionRows(items, "endpoints"),
  };
}

function unionRows(items, key) {
  const names = {};
  for (const item of items) {
    for (const row of item[key] || []) {
      names[row.name] = true;
    }
  }
  return Object.keys(names).sort().map((name) => {
    const values = {};
    for (const item of items) {
      const row = findByName(item[key], name);
      values[item.store] = row || null;
    }
    return { name, values };
  });
}

function markdownComparison(report) {
  const lines = [];
  const stores = report.stores.map((item) => item.store);
  lines.push("# RegiMux k6 Load Comparison");
  lines.push("");
  lines.push(`- generated_at: ${report.generated_at}`);
  lines.push(`- profile: ${report.profile}`);
  lines.push(`- base_url: ${report.base_url}`);
  lines.push(`- stores: ${stores.join(", ")}`);
  lines.push("");
  lines.push("## Sources");
  lines.push("");
  lines.push("| metadata store | report | generated_at |");
  lines.push("| --- | --- | --- |");
  for (const item of report.stores) {
    lines.push(`| ${item.store} | ${item.source} | ${item.generated_at || "n/a"} |`);
  }
  lines.push("");
  lines.push("## Overall");
  lines.push("");
  lines.push("| metadata store | requests | req/s | failed | avg | p95 | p99 | data received |");
  lines.push("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |");
  for (const item of report.stores) {
    lines.push([
      item.store,
      formatNumber(item.overall.requests, 0),
      formatNumber(item.overall.request_rate, 2),
      formatPercent(item.overall.failed_rate),
      formatMs(durationValue(item.overall, "avg")),
      formatMs(durationValue(item.overall, "p95")),
      formatMs(durationValue(item.overall, "p99")),
      formatBytes(item.overall.data_received_bytes),
    ].join(" | ").replace(/^/, "| ").replace(/$/, " |"));
  }
  addMetricMatrix(lines, "Scenario p95", report.scenarios, stores, (row) => formatMs(durationValue(row, "p95")));
  addMetricMatrix(lines, "Scenario Throughput", report.scenarios, stores, (row) => row ? formatNumber(row.request_rate, 2) : "n/a", "req/s");
  addMetricMatrix(lines, "Endpoint p95", report.endpoints, stores, (row) => formatMs(durationValue(row, "p95")));
  addMetricMatrix(lines, "Endpoint Throughput", report.endpoints, stores, (row) => row ? formatNumber(row.request_rate, 2) : "n/a", "req/s");
  lines.push("");
  lines.push("## Notes");
  lines.push("");
  lines.push("- Compare reports generated with the same `REGIMUX_STRESS_PROFILE`; mixed profiles are shown as `mixed`.");
  lines.push("- Cold scenario rows are short shared-iteration baselines; sustained throughput comparisons should focus on hot and mixed scenarios.");
  lines.push("- JSON output contains the same store, scenario, and endpoint tables for CI trend storage.");
  lines.push("");
  return `${lines.join("\n")}\n`;
}

function addMetricMatrix(lines, title, rows, stores, formatter, suffix) {
  lines.push("");
  lines.push(`## ${title}`);
  lines.push("");
  lines.push(`| name | ${stores.map((store) => suffix ? `${store} ${suffix}` : store).join(" | ")} |`);
  lines.push(`| --- | ${stores.map(() => "---:").join(" | ")} |`);
  for (const row of rows) {
    lines.push(`| ${row.name} | ${stores.map((store) => formatter(row.values[store])).join(" | ")} |`);
  }
}

function legacyRows(data, tag) {
  const prefix = `http_reqs{${tag}:`;
  const rows = [];
  for (const metricName in data.metrics || {}) {
    if (metricName.indexOf(prefix) !== 0) {
      continue;
    }
    const name = metricName.slice(prefix.length, -1);
    rows.push(Object.assign({ name }, legacyMetricRow(data, `{${tag}:${name}}`)));
  }
  return rows;
}

function legacyMetricRow(data, selector) {
  return {
    requests: valueOf(data, `http_reqs${selector}`, "count"),
    request_rate: valueOf(data, `http_reqs${selector}`, "rate"),
    failed_rate: valueOf(data, `http_req_failed${selector}`, "rate"),
    duration_ms: {
      avg: valueOf(data, `http_req_duration${selector}`, "avg"),
      p95: valueOf(data, `http_req_duration${selector}`, "p(95)"),
      p99: valueOf(data, `http_req_duration${selector}`, "p(99)"),
    },
    data_received_bytes: selector === "" ? valueOf(data, "data_received", "count") : null,
  };
}

function findByName(rows, name) {
  for (const row of rows || []) {
    if (row.name === name) {
      return row;
    }
  }
  return null;
}

function valueOf(data, metricName, valueName) {
  const metric = data.metrics[metricName];
  if (!metric || !metric.values || metric.values[valueName] === undefined) {
    return null;
  }
  return metric.values[valueName];
}

function durationValue(row, key) {
  return row && row.duration_ms ? row.duration_ms[key] : null;
}

function commonValue(values) {
  if (!values.length) {
    return "";
  }
  const first = values[0];
  for (const value of values) {
    if (value !== first) {
      return "";
    }
  }
  return first;
}

function formatMs(value) {
  if (value === null || value === undefined) {
    return "n/a";
  }
  return `${formatNumber(value, 2)} ms`;
}

function formatPercent(value) {
  if (value === null || value === undefined) {
    return "n/a";
  }
  return `${formatNumber(value * 100, 2)}%`;
}

function formatNumber(value, digits) {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return "n/a";
  }
  return Number(value).toFixed(digits);
}

function formatBytes(value) {
  if (value === null || value === undefined) {
    return "n/a";
  }
  const units = ["B", "KiB", "MiB", "GiB"];
  let size = Number(value);
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${formatNumber(size, unit === 0 ? 0 : 2)} ${units[unit]}`;
}

function cleanReportName(value) {
  return String(value || "").trim().replace(/[^A-Za-z0-9_.-]+/g, "-").replace(/^-+|-+$/g, "");
}
