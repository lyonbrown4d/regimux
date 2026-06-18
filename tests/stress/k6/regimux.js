import http from "k6/http";
import { check, fail, sleep } from "k6";
import { Trend } from "k6/metrics";

const BASE_URL = (__ENV.REGIMUX_BASE_URL || "http://regimux:8080").replace(/\/+$/, "");
const PROFILE = __ENV.REGIMUX_STRESS_PROFILE || "load";
const METADATA_STORE = cleanReportName(__ENV.REGIMUX_STRESS_META_STORE || __ENV.REGIMUX_META_DRIVER || "current");
const REPORT_DIR = __ENV.REGIMUX_K6_REPORT_DIR || "/stress/reports";
const REPORT_NAME = cleanReportName(__ENV.REGIMUX_K6_REPORT_NAME) || defaultReportName();
const THINK_TIME_SECONDS = Number(__ENV.REGIMUX_STRESS_SLEEP || "0");
const BLOB_RANGE = __ENV.REGIMUX_STRESS_BLOB_RANGE || "bytes=0-65535";

const manifestAccept = [
  "application/vnd.oci.image.index.v1+json",
  "application/vnd.oci.image.manifest.v1+json",
  "application/vnd.docker.distribution.manifest.list.v2+json",
  "application/vnd.docker.distribution.manifest.v2+json",
].join(", ");

const referrersAccept = "application/vnd.oci.image.index.v1+json";
const blobStatuses = [200, 206];
const warmupDuration = new Trend("regimux_warmup_duration", true);

const artifacts = {
  npm: {
    package: "lodash",
    version: "4.17.21",
  },
  pypi: {
    package: "six",
    version: "1.17.0",
  },
  maven: {
    group: "commons-io",
    artifact: "commons-io",
    version: "2.16.1",
  },
  container: {
    hot: { name: "busybox", repo: "library/busybox", reference: "1.36.1" },
    coldManifest: [{ name: "alpine", repo: "library/alpine", reference: "3.19" }],
    coldBlob: [{ name: "hello-world", repo: "library/hello-world", reference: "latest" }],
    multiRepo: [
      { name: "busybox", repo: "library/busybox", reference: "1.36.1" },
      { name: "alpine", repo: "library/alpine", reference: "3.19" },
      { name: "hello-world", repo: "library/hello-world", reference: "latest" },
    ],
  },
};

const profiles = {
  smoke: {
    baselineVus: 1,
    baselineDuration: "5s",
    coldVus: 1,
    coldIterations: 1,
    coldMaxDuration: "45s",
    isolatedVus: 2,
    isolatedDuration: "8s",
    sameBlobVus: 4,
    mixedVus: 4,
    mixedDuration: "10s",
    p95ThresholdMs: 5000,
    coldP95ThresholdMs: 45000,
  },
  load: {
    baselineVus: 2,
    baselineDuration: "10s",
    coldVus: 1,
    coldIterations: 1,
    coldMaxDuration: "60s",
    isolatedVus: 8,
    isolatedDuration: "20s",
    sameBlobVus: 24,
    mixedVus: 32,
    mixedDuration: "45s",
    p95ThresholdMs: 3000,
    coldP95ThresholdMs: 60000,
  },
  stress: {
    baselineVus: 4,
    baselineDuration: "15s",
    coldVus: 1,
    coldIterations: 1,
    coldMaxDuration: "90s",
    isolatedVus: 16,
    isolatedDuration: "30s",
    sameBlobVus: 48,
    mixedVus: 64,
    mixedDuration: "60s",
    p95ThresholdMs: 5000,
    coldP95ThresholdMs: 90000,
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
    name: "container_manifest_cold",
    exec: "containerManifestCold",
    description: "First OCI/Docker manifest request for a repo not warmed during setup.",
    executor: "shared-iterations",
    vus: profile.coldVus,
    iterations: profile.coldIterations,
    maxDuration: profile.coldMaxDuration,
    p95ThresholdMs: profile.coldP95ThresholdMs,
  },
  {
    name: "container_blob_cold",
    exec: "containerBlobCold",
    description: "First OCI/Docker blob range request after resolving a not-yet-cached repo manifest.",
    executor: "shared-iterations",
    vus: profile.coldVus,
    iterations: profile.coldIterations,
    maxDuration: profile.coldMaxDuration,
    p95ThresholdMs: profile.coldP95ThresholdMs,
  },
  {
    name: "npm_metadata_hot",
    exec: "npmMetadataHot",
    description: "npm package metadata from hot cache.",
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
    name: "container_blob_hot",
    exec: "containerBlobHot",
    description: "OCI/Docker blob range request from hot blob cache.",
    vus: profile.isolatedVus,
    duration: profile.isolatedDuration,
  },
  {
    name: "container_blob_same_digest_concurrent",
    exec: "containerBlobSameDigestConcurrent",
    description: "Concurrent range requests against the same OCI/Docker blob digest.",
    vus: profile.sameBlobVus,
    duration: profile.isolatedDuration,
  },
  {
    name: "container_multi_repo_mixed",
    exec: "containerMultiRepoMixed",
    description: "Mixed manifest, blob, and tag requests across multiple container repositories.",
    vus: profile.mixedVus,
    duration: profile.mixedDuration,
  },
  {
    name: "container_referrers_tags",
    exec: "containerReferrersTags",
    description: "OCI referrers and Docker tags/list requests.",
    vus: profile.isolatedVus,
    duration: profile.isolatedDuration,
  },
  {
    name: "mixed_ecosystems",
    exec: "mixedEcosystems",
    description: "Concurrent npm, PyPI, Maven, and container proxy requests.",
    vus: profile.mixedVus,
    duration: profile.mixedDuration,
  },
];

const endpointDefs = [
  { name: "health_baseline", description: "Readiness endpoint baseline." },
  { name: "container_manifest_cold", description: "Cold container manifest request." },
  { name: "container_blob_cold_manifest", description: "Manifest resolution before cold blob request." },
  { name: "container_blob_cold", description: "Cold container blob range request." },
  { name: "npm_metadata_hot", description: "Hot npm metadata request." },
  { name: "npm_tarball_hot", description: "Hot npm tarball body request." },
  { name: "pypi_simple_hot", description: "Hot PyPI simple index request." },
  { name: "pypi_wheel_hot", description: "Hot PyPI wheel body request." },
  { name: "maven_release_hot", description: "Hot Maven release body request." },
  { name: "container_manifest_hot", description: "Hot container manifest request." },
  { name: "container_blob_hot", description: "Hot container blob range request." },
  { name: "container_blob_same_digest_concurrent", description: "Concurrent same-digest blob range request." },
  { name: "container_multi_repo_manifest", description: "Multi-repo container manifest request." },
  { name: "container_multi_repo_blob_manifest", description: "Manifest resolution before multi-repo blob request." },
  { name: "container_multi_repo_blob", description: "Multi-repo container blob range request." },
  { name: "container_multi_repo_tags", description: "Multi-repo tags/list request." },
  { name: "container_tags", description: "Container tags/list request." },
  { name: "container_referrers", description: "Container referrers request." },
  { name: "mixed_npm_metadata", description: "Mixed npm metadata request." },
  { name: "mixed_npm_tarball", description: "Mixed npm tarball request." },
  { name: "mixed_pypi_simple", description: "Mixed PyPI simple request." },
  { name: "mixed_pypi_wheel", description: "Mixed PyPI wheel request." },
  { name: "mixed_maven_release", description: "Mixed Maven release request." },
  { name: "mixed_container_manifest", description: "Mixed container manifest request." },
  { name: "mixed_container_blob", description: "Mixed container blob range request." },
];

export const options = {
  discardResponseBodies: false,
  scenarios: buildScenarios(scenarioDefs),
  thresholds: buildThresholds(scenarioDefs, endpointDefs),
  summaryTrendStats: ["min", "avg", "med", "p(90)", "p(95)", "p(99)", "max"],
};

export function setup() {
  waitReady();

  const npmMetadataURL = `${BASE_URL}/npm/default/lodash`;
  const npmTarballURL = `${BASE_URL}/npm/default/lodash/-/lodash-4.17.21.tgz`;
  const pypiSimpleURL = `${BASE_URL}/pypi/default/simple/six/`;
  const mavenJarURL = `${BASE_URL}/maven/central/commons-io/commons-io/2.16.1/commons-io-2.16.1.jar`;

  warmup("npm_metadata", http.get(npmMetadataURL, tagged("warmup_npm_metadata")));
  warmup("npm_tarball", http.get(npmTarballURL, tagged("warmup_npm_tarball")));

  const pypiSimple = warmup("pypi_simple", http.get(pypiSimpleURL, tagged("warmup_pypi_simple")));
  const pypiWheelURL = findPyPIWheelURL(pypiSimple.body);
  warmup("pypi_wheel", http.get(pypiWheelURL, tagged("warmup_pypi_wheel")));

  warmup("maven_release", http.get(mavenJarURL, tagged("warmup_maven_release")));

  const hotTarget = artifacts.container.hot;
  const hotManifestURL = containerManifestURL(hotTarget);
  const hotManifest = warmup(
    "container_manifest",
    http.get(hotManifestURL, withHeaders({ Accept: manifestAccept }, "warmup_container_manifest")),
  );
  const resolvedHot = resolveImageManifestFromResponse(hotManifest, hotTarget, "warmup_container_image_manifest");
  const hotImageManifest = resolvedHot.response;
  const hotBlobURL = findContainerBlobURL(hotImageManifest.body, hotTarget);
  const hotManifestDigest = resolvedHot.digest || contentDigest(hotImageManifest) || referenceDigest(resolvedHot.url);

  warmup(
    "container_blob",
    http.get(hotBlobURL, withHeaders({ Range: BLOB_RANGE }, "warmup_container_blob")),
    blobStatuses,
  );

  return {
    npmMetadataURL,
    npmTarballURL,
    pypiSimpleURL,
    pypiWheelURL,
    mavenJarURL,
    container: {
      hot: {
        target: hotTarget,
        manifestURL: hotManifestURL,
        imageManifestURL: resolvedHot.url,
        manifestDigest: hotManifestDigest,
        blobURL: hotBlobURL,
      },
      coldManifestTargets: artifacts.container.coldManifest,
      coldBlobTargets: artifacts.container.coldBlob,
      multiRepoTargets: artifacts.container.multiRepo,
    },
  };
}

export function healthBaseline() {
  request("health_baseline", "GET", `${BASE_URL}/readyz`);
  maybeSleep();
}

export function containerManifestCold(data) {
  const target = pick(data.container.coldManifestTargets);
  request(
    "container_manifest_cold",
    "GET",
    containerManifestURL(target),
    withHeaders({ Accept: manifestAccept }, "container_manifest_cold"),
  );
  maybeSleep();
}

export function containerBlobCold(data) {
  const target = pick(data.container.coldBlobTargets);
  const manifest = request(
    "container_blob_cold_manifest",
    "GET",
    containerManifestURL(target),
    withHeaders({ Accept: manifestAccept }, "container_blob_cold_manifest"),
  );
  const resolved = resolveImageManifestFromResponse(manifest, target, "container_blob_cold_manifest");
  const blobURL = findContainerBlobURL(resolved.response.body, target);
  request(
    "container_blob_cold",
    "GET",
    blobURL,
    withHeaders({ Range: BLOB_RANGE }, "container_blob_cold"),
    blobStatuses,
  );
  maybeSleep();
}

export function npmMetadataHot(data) {
  request("npm_metadata_hot", "GET", data.npmMetadataURL);
  maybeSleep();
}

export function npmTarballHot(data) {
  request("npm_tarball_hot", "GET", data.npmTarballURL);
  maybeSleep();
}

export function pypiSimpleHot(data) {
  request("pypi_simple_hot", "GET", data.pypiSimpleURL);
  maybeSleep();
}

export function pypiWheelHot(data) {
  request("pypi_wheel_hot", "GET", data.pypiWheelURL);
  maybeSleep();
}

export function mavenReleaseHot(data) {
  request("maven_release_hot", "GET", data.mavenJarURL);
  maybeSleep();
}

export function containerManifestHot(data) {
  request("container_manifest_hot", "GET", data.container.hot.manifestURL, withHeaders({ Accept: manifestAccept }, "container_manifest_hot"));
  maybeSleep();
}

export function containerBlobHot(data) {
  request(
    "container_blob_hot",
    "GET",
    data.container.hot.blobURL,
    withHeaders({ Range: BLOB_RANGE }, "container_blob_hot"),
    blobStatuses,
  );
  maybeSleep();
}

export function containerBlobSameDigestConcurrent(data) {
  request(
    "container_blob_same_digest_concurrent",
    "GET",
    data.container.hot.blobURL,
    withHeaders({ Range: BLOB_RANGE }, "container_blob_same_digest_concurrent"),
    blobStatuses,
  );
  maybeSleep();
}

export function containerMultiRepoMixed(data) {
  const target = pick(data.container.multiRepoTargets);
  switch (__ITER % 3) {
    case 0:
      request(
        "container_multi_repo_manifest",
        "GET",
        containerManifestURL(target),
        withHeaders({ Accept: manifestAccept }, "container_multi_repo_manifest"),
      );
      break;
    case 1: {
      const manifest = request(
        "container_multi_repo_blob_manifest",
        "GET",
        containerManifestURL(target),
        withHeaders({ Accept: manifestAccept }, "container_multi_repo_blob_manifest"),
      );
      const resolved = resolveImageManifestFromResponse(manifest, target, "container_multi_repo_blob_manifest");
      const blobURL = findContainerBlobURL(resolved.response.body, target);
      request(
        "container_multi_repo_blob",
        "GET",
        blobURL,
        withHeaders({ Range: BLOB_RANGE }, "container_multi_repo_blob"),
        blobStatuses,
      );
      break;
    }
    default:
      request("container_multi_repo_tags", "GET", containerTagsURL(target), tagged("container_multi_repo_tags"));
  }
  maybeSleep();
}

export function containerReferrersTags(data) {
  if (__ITER % 2 === 0) {
    request("container_tags", "GET", containerTagsURL(data.container.hot.target), tagged("container_tags"));
    maybeSleep();
    return;
  }

  const digest = data.container.hot.manifestDigest;
  if (!digest) {
    fail("hot container manifest digest is required for referrers scenario");
  }
  request(
    "container_referrers",
    "GET",
    containerReferrersURL(data.container.hot.target, digest),
    withHeaders({ Accept: referrersAccept }, "container_referrers"),
    [200, 404],
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
      request("mixed_container_manifest", "GET", data.container.hot.manifestURL, withHeaders({ Accept: manifestAccept }, "mixed_container_manifest"));
      break;
    default:
      request(
        "mixed_container_blob",
        "GET",
        data.container.hot.blobURL,
        withHeaders({ Range: BLOB_RANGE }, "mixed_container_blob"),
        blobStatuses,
      );
  }
  maybeSleep();
}

export function handleSummary(data) {
  const report = buildReport(data);
  const markdown = markdownSummary(report);
  const json = `${JSON.stringify(report, null, 2)}\n`;
  const basePath = `${REPORT_DIR}/${REPORT_NAME}`;

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
    const scenario = {
      executor: def.executor || "constant-vus",
      startTime: `${startSeconds}s`,
      gracefulStop: "10s",
      exec: def.exec,
    };
    if (scenario.executor === "shared-iterations") {
      scenario.vus = def.vus;
      scenario.iterations = def.iterations;
      scenario.maxDuration = def.maxDuration;
    } else {
      scenario.vus = def.vus;
      scenario.duration = def.duration;
    }
    scenarios[def.name] = scenario;
    startSeconds += durationSeconds(def.duration || def.maxDuration);
  }
  return scenarios;
}

function buildThresholds(scenarios, endpoints) {
  const thresholds = {
    checks: ["rate>0.99"],
    http_req_failed: ["rate<0.01"],
    http_req_duration: [`p(95)<${profile.p95ThresholdMs}`],
    regimux_warmup_duration: ["p(95)<30000"],
  };
  for (const def of scenarios) {
    thresholds[`http_reqs{scenario:${def.name}}`] = ["count>0"];
    thresholds[`http_req_failed{scenario:${def.name}}`] = ["rate<0.01"];
    thresholds[`http_req_duration{scenario:${def.name}}`] = [`p(95)<${def.p95ThresholdMs || profile.p95ThresholdMs}`];
  }
  for (const def of endpoints) {
    thresholds[`http_reqs{endpoint:${def.name}}`] = ["count>0"];
    thresholds[`http_req_failed{endpoint:${def.name}}`] = ["rate<0.01"];
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

function pick(values) {
  if (!values || values.length === 0) {
    fail("stress target list is empty");
  }
  return values[__ITER % values.length];
}

function containerManifestURL(target) {
  return `${BASE_URL}/v2/hub/${target.repo}/manifests/${target.reference}`;
}

function containerBlobURL(target, digest) {
  return `${BASE_URL}/v2/hub/${target.repo}/blobs/${digest}`;
}

function containerTagsURL(target) {
  return `${BASE_URL}/v2/hub/${target.repo}/tags/list?n=100`;
}

function containerReferrersURL(target, digest) {
  return `${BASE_URL}/v2/hub/${target.repo}/referrers/${digest}`;
}

function findPyPIWheelURL(body) {
  const match = String(body).match(/href=["']([^"']*six-1\.17\.0-py2\.py3-none-any\.whl[^"']*)["']/i);
  if (!match) {
    fail("could not find six 1.17.0 wheel link in PyPI simple index");
  }
  return absoluteURL(match[1].replace(/&amp;/g, "&"));
}

function resolveImageManifestFromResponse(res, target, endpoint) {
  const payload = parseJSON(res.body, `${target.name} container manifest`);
  if (Array.isArray(payload.layers)) {
    return {
      response: res,
      url: containerManifestURL(target),
      digest: contentDigest(res),
    };
  }
  if (!Array.isArray(payload.manifests) || payload.manifests.length === 0) {
    fail(`${target.name} container manifest has neither layers nor manifest list entries`);
  }
  const selected = payload.manifests.find((item) => item.platform && item.platform.os === "linux" && item.platform.architecture === "amd64")
    || payload.manifests[0];
  if (!selected.digest) {
    fail(`${target.name} container manifest list entry has no digest`);
  }
  const digestTarget = {
    name: target.name,
    repo: target.repo,
    reference: selected.digest,
  };
  const url = containerManifestURL(digestTarget);
  const imageManifest = request(
    endpoint,
    "GET",
    url,
    withHeaders({ Accept: manifestAccept }, endpoint),
  );
  return {
    response: imageManifest,
    url,
    digest: selected.digest,
  };
}

function findContainerBlobURL(body, target) {
  const payload = parseJSON(body, `${target.name} container image manifest`);
  let digest = "";
  if (Array.isArray(payload.layers) && payload.layers.length > 0) {
    digest = payload.layers[0].digest;
  } else if (payload.config && payload.config.digest) {
    digest = payload.config.digest;
  }
  if (!digest) {
    fail(`${target.name} container image manifest contains no blob digest`);
  }
  return containerBlobURL(target, digest);
}

function parseJSON(body, label) {
  try {
    return JSON.parse(String(body));
  } catch (err) {
    fail(`failed to parse ${label} JSON: ${err}`);
  }
}

function contentDigest(res) {
  return headerValue(res, "Docker-Content-Digest");
}

function headerValue(res, name) {
  const want = String(name).toLowerCase();
  const headers = res.headers || {};
  for (const key in headers) {
    if (String(key).toLowerCase() === want) {
      return String(headers[key]);
    }
  }
  return "";
}

function referenceDigest(url) {
  const match = String(url).match(/\/manifests\/(sha256:[A-Fa-f0-9]{64})$/);
  return match ? match[1] : "";
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

function buildReport(data) {
  const generatedAt = new Date().toISOString();
  return {
    schema_version: 1,
    generated_at: generatedAt,
    report_name: REPORT_NAME,
    profile: PROFILE,
    metadata_store: METADATA_STORE,
    base_url: BASE_URL,
    blob_range: BLOB_RANGE,
    artifacts,
    overall: metricRow(data, ""),
    scenarios: scenarioDefs.map((def) => Object.assign({}, scenarioInfo(def), metricRow(data, `{scenario:${def.name}}`))),
    endpoints: endpointDefs.map((def) => Object.assign({}, def, metricRow(data, `{endpoint:${def.name}}`))),
    warmup: warmupRow(data),
    thresholds: thresholdSummary(data),
    k6_summary: data,
  };
}

function scenarioInfo(def) {
  return {
    name: def.name,
    executor: def.executor || "constant-vus",
    vus: def.vus,
    iterations: def.iterations || null,
    duration: def.duration || null,
    max_duration: def.maxDuration || null,
    description: def.description,
  };
}

function metricRow(data, selector) {
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

function warmupRow(data) {
  return {
    duration_ms: {
      min: valueOf(data, "regimux_warmup_duration", "min"),
      avg: valueOf(data, "regimux_warmup_duration", "avg"),
      med: valueOf(data, "regimux_warmup_duration", "med"),
      p90: valueOf(data, "regimux_warmup_duration", "p(90)"),
      p95: valueOf(data, "regimux_warmup_duration", "p(95)"),
      p99: valueOf(data, "regimux_warmup_duration", "p(99)"),
      max: valueOf(data, "regimux_warmup_duration", "max"),
    },
  };
}

function thresholdSummary(data) {
  const result = [];
  const root = data.root_group || {};
  const checks = root.checks || [];
  for (const item of checks) {
    result.push({
      name: item.name,
      path: item.path,
      passes: item.passes,
      fails: item.fails,
    });
  }
  return result;
}

function markdownSummary(report) {
  const lines = [];
  lines.push("# RegiMux k6 Stress Report");
  lines.push("");
  lines.push(`- profile: ${report.profile}`);
  lines.push(`- metadata_store: ${report.metadata_store}`);
  lines.push(`- base_url: ${report.base_url}`);
  lines.push(`- generated_at: ${report.generated_at}`);
  lines.push(`- report_name: ${report.report_name}`);
  lines.push(`- blob_range: ${report.blob_range}`);
  lines.push("");
  lines.push("## Overall");
  lines.push("");
  lines.push("| metric | value |");
  lines.push("| --- | ---: |");
  lines.push(`| requests | ${formatNumber(report.overall.requests, 0)} |`);
  lines.push(`| request_rate | ${formatNumber(report.overall.request_rate, 2)}/s |`);
  lines.push(`| failed_rate | ${formatPercent(report.overall.failed_rate)} |`);
  lines.push(`| duration_avg | ${formatMs(report.overall.duration_ms.avg)} |`);
  lines.push(`| duration_p95 | ${formatMs(report.overall.duration_ms.p95)} |`);
  lines.push(`| duration_p99 | ${formatMs(report.overall.duration_ms.p99)} |`);
  lines.push(`| data_received | ${formatBytes(report.overall.data_received_bytes)} |`);
  lines.push("");
  lines.push("## Scenarios");
  lines.push("");
  lines.push("| scenario | executor | vus | duration | iterations | requests | req/s | failed | avg | p95 | p99 | notes |");
  lines.push("| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |");
  for (const row of report.scenarios) {
    lines.push([
      row.name,
      row.executor,
      row.vus,
      row.duration || row.max_duration || "n/a",
      row.iterations === null ? "n/a" : row.iterations,
      formatNumber(row.requests, 0),
      formatNumber(row.request_rate, 2),
      formatPercent(row.failed_rate),
      formatMs(row.duration_ms.avg),
      formatMs(row.duration_ms.p95),
      formatMs(row.duration_ms.p99),
      row.description,
    ].join(" | ").replace(/^/, "| ").replace(/$/, " |"));
  }
  lines.push("");
  lines.push("## Endpoints");
  lines.push("");
  lines.push("| endpoint | requests | req/s | failed | avg | p95 | p99 | notes |");
  lines.push("| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |");
  for (const row of report.endpoints) {
    lines.push([
      row.name,
      formatNumber(row.requests, 0),
      formatNumber(row.request_rate, 2),
      formatPercent(row.failed_rate),
      formatMs(row.duration_ms.avg),
      formatMs(row.duration_ms.p95),
      formatMs(row.duration_ms.p99),
      row.description,
    ].join(" | ").replace(/^/, "| ").replace(/$/, " |"));
  }
  lines.push("");
  lines.push("## Warmup");
  lines.push("");
  lines.push("Warmup requests populate npm, PyPI, Maven, and the hot OCI manifest/blob targets before hot-path scenarios run.");
  lines.push("");
  lines.push("| metric | value |");
  lines.push("| --- | ---: |");
  lines.push(`| warmup_avg | ${formatMs(report.warmup.duration_ms.avg)} |`);
  lines.push(`| warmup_p95 | ${formatMs(report.warmup.duration_ms.p95)} |`);
  lines.push(`| warmup_max | ${formatMs(report.warmup.duration_ms.max)} |`);
  lines.push("");
  lines.push("## Interpretation");
  lines.push("");
  lines.push("- Cold scenarios are short shared-iteration baselines and should be read separately from sustained hot-cache throughput.");
  lines.push("- Hot scenarios show single endpoint behavior after setup has populated artifact cache state.");
  lines.push("- `container_blob_same_digest_concurrent` isolates contention on one blob digest.");
  lines.push("- `container_multi_repo_mixed` mixes manifest, blob, and tags/list traffic across busybox, alpine, and hello-world.");
  lines.push("- Use `task stress:databases` and the comparison report to evaluate sqlite, MySQL, and Postgres metadata-store impact with the same profile.");
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
  return cleanReportName(`regimux-stress-${METADATA_STORE}-${PROFILE}-${new Date().toISOString().replace(/[:.]/g, "-")}`);
}
