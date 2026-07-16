import assert from "node:assert/strict";
import test from "node:test";

import { buildThresholds, metricRow } from "../k6/reporting.js";

test("buildThresholds retains endpoint duration submetrics without adding an endpoint SLA", () => {
  const thresholds = buildThresholds(
    { p95ThresholdMs: 3000 },
    [{ name: "mixed", p95ThresholdMs: 5000 }],
    [{ name: "npm_metadata_hot" }],
  );

  assert.deepEqual(thresholds["http_req_duration{endpoint:npm_metadata_hot}"], ["p(95)>=0"]);
  assert.deepEqual(thresholds["http_req_duration{scenario:mixed}"], ["p(95)<5000"]);
});

test("metricRow reads endpoint throughput and p95 from the same tagged selector", () => {
  const selector = "{endpoint:npm_metadata_hot}";
  const data = {
    metrics: {
      [`http_reqs${selector}`]: { values: { count: 12, rate: 4.5 } },
      [`http_req_failed${selector}`]: { values: { rate: 0 } },
      [`http_req_duration${selector}`]: {
        values: {
          min: 1,
          avg: 5,
          med: 4,
          "p(90)": 8,
          "p(95)": 9,
          "p(99)": 11,
          max: 12,
        },
      },
    },
  };

  const row = metricRow(data, selector);

  assert.equal(row.requests, 12);
  assert.equal(row.request_rate, 4.5);
  assert.equal(row.duration_ms.p95, 9);
});