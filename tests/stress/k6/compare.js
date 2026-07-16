import {
  buildComparison,
  markdownComparison,
  normalizeReport,
} from "./comparison.js";

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
  const comparison = buildComparison(reports, REPORT_NAME, new Date().toISOString());
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
  return normalizeReport(JSON.parse(open(spec.path)), spec);
}

function cleanReportName(value) {
  return String(value || "").trim().replace(/[^A-Za-z0-9_.-]+/g, "-").replace(/^-+|-+$/g, "");
}