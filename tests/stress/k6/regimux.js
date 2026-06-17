import http from "k6/http";
import { check, fail, sleep } from "k6";
import { Trend } from "k6/metrics";

const BASE_URL = (__ENV.REGIMUX_BASE_URL || "http://regimux:8080").replace(/\/+$/, "");
const PROFILE = __ENV.REGIMUX_STRESS_PROFILE || "load";
const REPORT_DIR = __ENV.REGIMUX_K6_REPORT_DIR || "/stress/reports";
const REPORT_NAME = cleanReportName(__ENV.REGIMUX_K6_REPORT_NAME) || defaultReportName();
const THINK_TIME_SECONDS = Number(__ENV.REGIMUX_STRESS_SLEEP || "0");

const manifestAccept = [
  "application/vnd.oci.image.index.v1+json",
  "application/vnd.oci.image.manifest.v1+json",
  "application/vnd.docker.distribution.manifest.list.v2+json",
  "application/vnd.docker.distribution.manifest.v2+json",
].join(", ");

const warmupDuration = new Trend("regimux_warmup_duration", true);

const profiles = {
  smoke: {
    baselineVus: 1,
    baselineDuration: "5s",
    isolatedVus: 2,
    isolatedDuration: "8s",
    mixedVus: 4,
    mixedDuration: "10s",
    p95ThresholdMs: 5000,
  },
  load: {
    baselineVus: 2,
    baselineDuration: "10s",
    isolatedVus: 8,
    isolatedDuration: "20s",
    mixedVus: 32,
    mixedDuration: "45s",
    p95ThresholdMs: 3000,
  },
  stress: {
    baselineVus: 4,
    baselineDuration: "15s",
    isolatedVus: 16,
    isolatedDuration: "30s",
    mixedVus: 64,
    mixedDuration: "60s",
    p95ThresholdMs: 5000,
  },
};

const profile = profiles[PROFILE] || profiles.load;

const scenarioDefs = [
  {
    name: "health_baseline",
    exec: "healthBaseline",
    description: "Healthcheck baseline without artifact body work.",
    vus: profile.baselineVus,
    duration: profile.baselineDuration,
  },
  {
    name: "npm_metadata_hot",
    exec: "npmMetadataHot",
    description: "npm package metadata from Redis/object metadata hot path.",
    vus: profile.isolatedVus,
    duration: profile.isolatedDuration,
  },
  {
    name: "npm_tarball_hot",
    exec: "npmTarballHot",
    description: "npm tarball body download from hot artifact cache.",
    vus: profile.isolatedVus,
    duration: profile.isolatedDuration,
  },
  {
    name: "pypi_simple_hot",
    exec: "pypiSimpleHot",
    description: "PyPI simple index from hot metadata cache.",
    vus: profile.isolatedVus,
    duration: profile.isolatedDuration,
  },
  {
    name: "pypi_wheel_hot",
    exec: "pypiWheelHot",
    description: "PyPI wheel body download from hot artifact cache.",
    vus: profile.isolatedVus,
    duration: profile.isolatedDuration,
  },
  {
    name: "maven_release_hot",
    exec: "mavenReleaseHot",
    description: "Maven release JAR body download from hot artifact cache.",
    vus: profile.isolatedVus,
    duration: profile.isolatedDuration,
  },
  {
    name: "container_manifest_hot",
    exec: "containerManifestHot",
    description: "OCI/Docker manifest request from hot manifest cache.",
    vus: profile.isolatedVus,
    duration: profile.isolatedDuration,
  },
  {
    name: "container_blob_range_hot",
    exec: "containerBlobRangeHot",
    description: "OCI/Docker blob range request from streamed blob cache.",
    vus: profile.isolatedVus,
    duration: profile.isolatedDuration,
  },
  {
    name: "mixed_ecosystems",
    exec: "mixedEcosystems",
    description: "Concurrent mixed npm, PyPI, Maven, and container proxy requests.",
    vus: profile.mixedVus,
    duration: profile.mixedDuration,
  },
];

export const options = {
  discardResponseBodies: false,
  scenarios: buildScenarios(scenarioDefs),
  thresholds: buildThresholds(scenarioDefs),
  summaryTrendStats: ["min", "avg", "med", "p(90)", "p(95)", "p(99)", "max"],
};

export function setup() {
  waitReady();

  const npmMetadataURL = `${BASE_URL}/npm/default/lodash`;
  const npmTarballURL = `${BASE_URL}/npm/default/lodash/-/lodash-4.17.21.tgz`;
  const pypiSimpleURL = `${BASE_URL}/pypi/default/simple/six/`;
  const mavenJarURL = `${BASE_URL}/maven/central/commons-io/commons-io/2.16.1/commons-io-2.16.1.jar`;
  const containerManifestURL = `${BASE_URL}/v2/hub/library/busybox/manifests/1.36.1`;

  warmup("npm_metadata", http.get(npmMetadataURL, tagged("warmup_npm_metadata")));
  warmup("npm_tarball", http.get(npmTarballURL, tagged("warmup_npm_tarball")));

  const pypiSimple = warmup("pypi_simple", http.get(pypiSimpleURL, tagged("warmup_pypi_simple")));
  const pypiWheelURL = findPyPIWheelURL(pypiSimple.body);
  warmup("pypi_wheel", http.get(pypiWheelURL, tagged("warmup_pypi_wheel")));

  warmup("maven_release", http.get(mavenJarURL, tagged("warmup_maven_release")));

  const manifest = warmup(
    "container_manifest",
    http.get(containerManifestURL, withHeaders({ Accept: manifestAccept }, "warmup_container_manifest")),
  );
  const imageManifestURL = resolveImageManifestURL(manifest.body, containerManifestURL);
  const imageManifest = imageManifestURL === containerManifestURL
    ? manifest
    : warmup("container_image_manifest", http.get(imageManifestURL, withHeaders({ Accept: manifestAccept }, "warmup_container_image_manifest")));
  const blobURL = findContainerBlobURL(imageManifest.body);
  warmup(
    "container_blob_range",
    http.get(blobURL, withHeaders({ Range: "bytes=0-65535" }, "warmup_container_blob_range")),
    [206],
  );

  return {
    npmMetadataURL,
    npmTarballURL,
    pypiSimpleURL,
    pypiWheelURL,
    mavenJarURL,
    containerManifestURL,
    containerBlobURL: blobURL,
  };
}

export function healthBaseline() {
  request("health_baseline", "GET", `${BASE_URL}/readyz`);
  maybeSleep();
}

export function npmMetadataHot(data) {
  request("npm_metadata", "GET", data.npmMetadataURL);
  maybeSleep();
}

export function npmTarballHot(data) {
  request("npm_tarball", "GET", data.npmTarballURL);
  maybeSleep();
}

export function pypiSimpleHot(data) {
  request("pypi_simple", "GET", data.pypiSimpleURL);
  maybeSleep();
}

export function pypiWheelHot(data) {
  request("pypi_wheel", "GET", data.pypiWheelURL);
  maybeSleep();
}

export function mavenReleaseHot(data) {
  request("maven_release", "GET", data.mavenJarURL);
  maybeSleep();
}

export function containerManifestHot(data) {
  request("container_manifest", "GET", data.containerManifestURL, withHeaders({ Accept: manifestAccept }, "container_manifest"));
  maybeSleep();
}

export function containerBlobRangeHot(data) {
  request(
    "container_blob_range",
    "GET",
    data.containerBlobURL,
    withHeaders({ Range: "bytes=0-65535" }, "container_blob_range"),
    [206],
  );
  maybeSleep();
}

export function mixedEcosystems(data) {
  switch (__ITER % 7) {
    case 0:
      request("mixed_npm_metadata", "GET", data.npmMetadataURL);
      break;
    case 1:
      request("mixed_npm_tarball", "GET", data.npmTarballURL);
      break;
    case 2:
      request("mixed_pypi_simple", "GET", data.pypiSimpleURL);
      break;
    case 3:
      request("mixed_pypi_wheel", "GET", data.pypiWheelURL);
      break;
    case 4:
      request("mixed_maven_release", "GET", data.mavenJarURL);
      break;
    case 5:
      request("mixed_container_manifest", "GET", data.containerManifestURL, withHeaders({ Accept: manifestAccept }, "mixed_container_manifest"));
      break;
    default:
      request(
        "mixed_container_blob_range",
        "GET",
        data.containerBlobURL,
        withHeaders({ Range: "bytes=0-65535" }, "mixed_container_blob_range"),
        [206],
      );
  }
  maybeSleep();
}

export function handleSummary(data) {
  const basePath = `${REPORT_DIR}/${REPORT_NAME}`;
  const markdown = markdownSummary(data);
  const json = JSON.stringify(data, null, 2);

  return {
    stdout: markdown,
    [`${basePath}.md`]: markdown,
    [`${basePath}.json`]: json,
  };
}

function buildScenarios(defs) {
  const scenarios = {};
  let startSeconds = 0;
  for (const def of defs) {
    scenarios[def.name] = {
      executor: "constant-vus",
      vus: def.vus,
      duration: def.duration,
      startTime: `${startSeconds}s`,
      gracefulStop: "10s",
      exec: def.exec,
    };
    startSeconds += durationSeconds(def.duration);
  }
  return scenarios;
}

function buildThresholds(defs) {
  const thresholds = {
    checks: ["rate>0.99"],
    http_req_failed: ["rate<0.01"],
    http_req_duration: [`p(95)<${profile.p95ThresholdMs}`],
    regimux_warmup_duration: ["p(95)<30000"],
  };
  for (const def of defs) {
    thresholds[`http_reqs{scenario:${def.name}}`] = ["count>0"];
    thresholds[`http_req_failed{scenario:${def.name}}`] = ["rate<0.01"];
    thresholds[`http_req_duration{scenario:${def.name}}`] = [`p(95)<${profile.p95ThresholdMs}`];
  }
  return thresholds;
}

function waitReady() {
  for (let i = 0; i < 60; i += 1) {
    const res = http.get(`${BASE_URL}/readyz`, tagged("readyz"));
    if (res.status === 200) {
      return;
    }
    sleep(1);
  }
  fail(`RegiMux did not become ready at ${BASE_URL}/readyz`);
}

function request(name, method, url, params = tagged(name), expected = [200]) {
  const res = http.request(method, url, null, params);
  const ok = check(res, {
    [`${name} status`]: (r) => expected.indexOf(r.status) >= 0,
    [`${name} body`]: (r) => method === "HEAD" || r.body !== null,
  });
  if (!ok) {
    console.error(`${name} ${method} ${url} returned ${res.status}: ${String(res.body).slice(0, 200)}`);
  }
  return res;
}

function warmup(name, res, expected = [200]) {
  warmupDuration.add(res.timings.duration, { endpoint: name });
  const ok = check(res, {
    [`warmup ${name} status`]: (r) => expected.indexOf(r.status) >= 0,
  });
  if (!ok) {
    fail(`warmup ${name} failed with ${res.status}: ${String(res.body).slice(0, 300)}`);
  }
  return res;
}

function tagged(name) {
  return { tags: { endpoint: name } };
}

function withHeaders(headers, endpoint) {
  return { headers, tags: { endpoint } };
}

function maybeSleep() {
  if (THINK_TIME_SECONDS > 0) {
    sleep(THINK_TIME_SECONDS);
  }
}

function findPyPIWheelURL(body) {
  const match = String(body).match(/href=["']([^"']*six-1\.17\.0-py2\.py3-none-any\.whl[^"']*)["']/i);
  if (!match) {
    fail("could not find six 1.17.0 wheel link in PyPI simple index");
  }
  return absoluteURL(match[1].replace(/&amp;/g, "&"));
}

function resolveImageManifestURL(body, originalURL) {
  const payload = parseJSON(body, "container manifest");
  if (Array.isArray(payload.layers)) {
    return originalURL;
  }
  if (!Array.isArray(payload.manifests) || payload.manifests.length === 0) {
    fail("container manifest has neither layers nor manifest list entries");
  }
  const selected = payload.manifests.find((item) => item.platform && item.platform.os === "linux" && item.platform.architecture === "amd64")
    || payload.manifests[0];
  if (!selected.digest) {
    fail("container manifest list entry has no digest");
  }
  return `${BASE_URL}/v2/hub/library/busybox/manifests/${selected.digest}`;
}

function findContainerBlobURL(body) {
  const payload = parseJSON(body, "container image manifest");
  let digest = "";
  if (Array.isArray(payload.layers) && payload.layers.length > 0) {
    digest = payload.layers[0].digest;
  } else if (payload.config && payload.config.digest) {
    digest = payload.config.digest;
  }
  if (!digest) {
    fail("container image manifest contains no blob digest");
  }
  return `${BASE_URL}/v2/hub/library/busybox/blobs/${digest}`;
}

function parseJSON(body, label) {
  try {
    return JSON.parse(String(body));
  } catch (err) {
    fail(`failed to parse ${label} JSON: ${err}`);
  }
}

function absoluteURL(value) {
  if (/^https?:\/\//i.test(value)) {
    return value;
  }
  if (value.startsWith("/")) {
    return `${BASE_URL}${value}`;
  }
  return `${BASE_URL}/${value}`;
}

function durationSeconds(value) {
  const match = String(value).match(/^(\d+)(ms|s|m)$/);
  if (!match) {
    fail(`unsupported duration: ${value}`);
  }
  const amount = Number(match[1]);
  switch (match[2]) {
    case "ms":
      return Math.ceil(amount / 1000);
    case "m":
      return amount * 60;
    default:
      return amount;
  }
}

function markdownSummary(data) {
  const lines = [];
  lines.push(`# RegiMux k6 Stress Report`);
  lines.push("");
  lines.push(`- profile: ${PROFILE}`);
  lines.push(`- base_url: ${BASE_URL}`);
  lines.push(`- generated_at: ${new Date().toISOString()}`);
  lines.push(`- report_name: ${REPORT_NAME}`);
  lines.push("");
  lines.push("## Overall");
  lines.push("");
  lines.push("| metric | value |");
  lines.push("| --- | ---: |");
  lines.push(`| requests | ${formatNumber(valueOf(data, "http_reqs", "count"), 0)} |`);
  lines.push(`| request_rate | ${formatNumber(valueOf(data, "http_reqs", "rate"), 2)}/s |`);
  lines.push(`| failed_rate | ${formatPercent(valueOf(data, "http_req_failed", "rate"))} |`);
  lines.push(`| duration_avg | ${formatMs(valueOf(data, "http_req_duration", "avg"))} |`);
  lines.push(`| duration_p95 | ${formatMs(valueOf(data, "http_req_duration", "p(95)"))} |`);
  lines.push(`| duration_p99 | ${formatMs(valueOf(data, "http_req_duration", "p(99)"))} |`);
  lines.push(`| data_received | ${formatBytes(valueOf(data, "data_received", "count"))} |`);
  lines.push("");
  lines.push("## Scenarios");
  lines.push("");
  lines.push("| scenario | vus | duration | requests | req/s | failed | avg | p95 | p99 | notes |");
  lines.push("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |");
  for (const def of scenarioDefs) {
    const scenario = def.name;
    lines.push([
      scenario,
      def.vus,
      def.duration,
      formatNumber(valueOf(data, `http_reqs{scenario:${scenario}}`, "count"), 0),
      formatNumber(valueOf(data, `http_reqs{scenario:${scenario}}`, "rate"), 2),
      formatPercent(valueOf(data, `http_req_failed{scenario:${scenario}}`, "rate")),
      formatMs(valueOf(data, `http_req_duration{scenario:${scenario}}`, "avg")),
      formatMs(valueOf(data, `http_req_duration{scenario:${scenario}}`, "p(95)")),
      formatMs(valueOf(data, `http_req_duration{scenario:${scenario}}`, "p(99)")),
      def.description,
    ].join(" | ").replace(/^/, "| ").replace(/$/, " |"));
  }
  lines.push("");
  lines.push("## Warmup");
  lines.push("");
  lines.push("Warmup requests populate real npm, PyPI, Maven, and OCI artifacts before hot-path scenarios run.");
  lines.push("");
  lines.push("| metric | value |");
  lines.push("| --- | ---: |");
  lines.push(`| warmup_avg | ${formatMs(valueOf(data, "regimux_warmup_duration", "avg"))} |`);
  lines.push(`| warmup_p95 | ${formatMs(valueOf(data, "regimux_warmup_duration", "p(95)"))} |`);
  lines.push(`| warmup_max | ${formatMs(valueOf(data, "regimux_warmup_duration", "max"))} |`);
  lines.push("");
  lines.push("## Interpretation");
  lines.push("");
  lines.push("- Isolated scenarios show single-ecosystem hot-cache behavior.");
  lines.push("- `mixed_ecosystems` shows contention when npm, PyPI, Maven, and container requests share the same RegiMux process, Redis cache, object store, and metadata store.");
  lines.push("- Compare sqlite, MySQL, and Postgres reports with the same profile to evaluate metadata-store impact.");
  lines.push("");
  return `${lines.join("\n")}\n`;
}

function valueOf(data, metricName, valueName) {
  const metric = data.metrics[metricName];
  if (!metric || !metric.values || metric.values[valueName] === undefined) {
    return null;
  }
  return metric.values[valueName];
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

function defaultReportName() {
  return cleanReportName(`regimux-stress-${PROFILE}-${new Date().toISOString().replace(/[:.]/g, "-")}`);
}
