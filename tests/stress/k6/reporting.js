export function buildThresholds(profile, scenarios, endpoints) {
  const thresholds = {
    checks: ["rate>0.99"],
    http_req_failed: ["rate<0.01"],
    http_req_duration: [`p(95)<${profile.p95ThresholdMs}`],
    regimux_warmup_duration: ["p(95)<30000"],
    regimux_artifact_integrity_failed: ["rate==0"],
  };
  for (const def of scenarios) {
    thresholds[`http_reqs{scenario:${def.name}}`] = ["count>0"];
    thresholds[`http_req_failed{scenario:${def.name}}`] = ["rate<0.01"];
    thresholds[`http_req_duration{scenario:${def.name}}`] = [
      `p(95)<${def.p95ThresholdMs || profile.p95ThresholdMs}`,
    ];
  }
  for (const def of endpoints) {
    thresholds[`http_reqs{endpoint:${def.name}}`] = ["count>0"];
    thresholds[`http_req_failed{endpoint:${def.name}}`] = ["rate<0.01"];
    thresholds[`http_req_duration{endpoint:${def.name}}`] = ["p(95)>=0"];
  }
  return thresholds;
}

export function metricRow(data, selector) {
  return {
    requests: valueOf(data, `http_reqs${selector}`, "count"),
    request_rate: valueOf(data, `http_reqs${selector}`, "rate"),
    failed_rate: valueOf(data, `http_req_failed${selector}`, "rate"),
    duration_ms: {
      min: valueOf(data, `http_req_duration${selector}`, "min"),
      avg: valueOf(data, `http_req_duration${selector}`, "avg"),
      med: valueOf(data, `http_req_duration${selector}`, "med"),
      p90: valueOf(data, `http_req_duration${selector}`, "p(90)"),
      p95: valueOf(data, `http_req_duration${selector}`, "p(95)"),
      p99: valueOf(data, `http_req_duration${selector}`, "p(99)"),
      max: valueOf(data, `http_req_duration${selector}`, "max"),
    },
    data_received_bytes: selector === "" ? valueOf(data, "data_received", "count") : null,
  };
}

function valueOf(data, metricName, valueName) {
  const metric = data.metrics[metricName];
  if (!metric || !metric.values || metric.values[valueName] === undefined) {
    return null;
  }
  return metric.values[valueName];
}