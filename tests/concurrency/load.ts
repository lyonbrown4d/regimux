import http from 'k6/http';
import crypto from 'k6/crypto';
import { check, fail } from 'k6';
import { Counter, Trend } from 'k6/metrics';

type Target = {
  name: string;
  ecosystem: string;
  path: string;
  upstream_path: string;
  status: number;
  sha256?: string;
  markers?: string[];
};
type Fixtures = { wave: string; targets: Target[] };
type Stats = { requests: Record<string, { GET: number; HEAD: number }> };
type Setup = { targets: Target[]; before: Stats };

const mode = __ENV.MODE || 'burst';
const wave = __ENV.WAVE || 'default';
const selectedName = __ENV.TARGET || 'all';
const concurrency = Number(__ENV.CONCURRENCY || '256');
const duration = __ENV.DURATION || '30s';
const fixtureURL = 'http://fixture:8081';
const bases = (__ENV.BASE_URLS || 'http://regimux:8080').split(',');
const report = __ENV.REPORT || 'concurrency-summary';
const mixedMethods = __ENV.MIXED_METHODS === '1';

if (!['burst', 'hot'].includes(mode)) throw new Error('MODE must be burst or hot');
if (!Number.isInteger(concurrency) || concurrency < 1 || concurrency > 2048) {
  throw new Error('CONCURRENCY must be an integer between 1 and 2048');
}
if (!/^[a-z0-9][a-z0-9-]{0,47}$/.test(wave)) throw new Error('Invalid WAVE');
if (!/^[a-z0-9._-]+$/i.test(report)) throw new Error('REPORT must be a filename');
if (!/^\d+[sm]$/.test(duration)) throw new Error('DURATION must use seconds or minutes');
if (bases.some(base => !/^http:\/\/regimux(?:-peer)?:8080$/.test(base))) {
  throw new Error('BASE_URLS must refer to isolated Compose services');
}

const corruptResponses = new Counter('corrupt_responses');
const cacheMisses = new Counter('cache_misses');
const successfulResponses = new Counter('successful_responses');
const upstreamFetches = new Counter('upstream_fetches');
const latencies: Record<string, Trend> = {
  oci: new Trend('oci_duration', true),
  go: new Trend('go_duration', true),
  npm: new Trend('npm_duration', true),
  pypi: new Trend('pypi_duration', true),
  maven: new Trend('maven_duration', true),
  dist: new Trend('dist_duration', true),
};

export const options = {
  scenarios: {
    load: mode === 'burst'
      ? { executor: 'per-vu-iterations', vus: concurrency, iterations: 1, maxDuration: '120s' }
      : { executor: 'constant-vus', vus: concurrency, duration, gracefulStop: '45s' },
  },
  thresholds: {
    http_req_failed: ['rate==0'],
    checks: ['rate==1'],
    corrupt_responses: ['count==0'],
  },
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  systemTags: ['status', 'method', 'name', 'scenario', 'expected_response'],
};

function fetchJSON(path: string): unknown {
  const response = http.get(fixtureURL + path, { timeout: '15s', tags: { name: 'fixture-control' } });
  if (response.status !== 200) fail(`Fixture control failed: ${path}: ${response.status}`);
  return response.json();
}

function request(base: string, target: Target, measured = true): boolean {
  const method = measured && mixedMethods && __VU % 2 === 0 ? 'HEAD' : 'GET';
  const response = http.request(method, base + target.path, null, {
    timeout: '45s',
    responseType: target.sha256 ? 'binary' : 'text',
    tags: { name: target.name, ecosystem: target.ecosystem, phase: measured ? mode : 'warmup' },
  });
  const body = response.body;
  const bodyOK = method === 'HEAD'
    ? body === null || (typeof body === 'string' ? body.length === 0 : body.byteLength === 0)
    : target.sha256
      ? body !== null && crypto.sha256(body, 'hex') === target.sha256
      : typeof body === 'string'
        && (target.markers || []).every(marker => body.includes(marker))
        && !body.includes('http://fixture:8081')
        && !body.includes('https://fixture.invalid');
  const ok = check(response, {
    'expected status': value => value.status === target.status,
    'complete validated fixture body': () => bodyOK,
  }, { ecosystem: target.ecosystem, target: target.name });
  corruptResponses.add(bodyOK ? 0 : 1, { ecosystem: target.ecosystem, target: target.name });
  if (measured) {
    successfulResponses.add(ok ? 1 : 0, { ecosystem: target.ecosystem, target: target.name });
    latencies[target.ecosystem].add(response.timings.duration);
  }
  if (measured && response.headers['X-Mirror-Cache'] === 'miss') {
    cacheMisses.add(1, { ecosystem: target.ecosystem, target: target.name });
  }
  if (!ok) console.error(JSON.stringify({
    target: target.name, status: response.status, bodyOK, error: response.error,
    errorCode: response.error_code, durationMs: response.timings.duration,
    vu: __VU, iteration: __ITER, cache: response.headers['X-Mirror-Cache'],
  }));
  return ok;
}

export function setup(): Setup {
  const fixtures = fetchJSON(`/__fixtures?wave=${wave}`) as Fixtures;
  const targets = fixtures.targets.filter(target => selectedName === 'all' || target.name === selectedName);
  if (!targets.length) fail(`No fixture matches TARGET=${selectedName}`);
  if (mode === 'burst' && concurrency < targets.length) fail('Burst needs at least one VU per target');
  for (const target of targets) {
    if (!target.path.startsWith('/') || target.path.startsWith('//')) fail('Invalid fixture proxy path');
    if (!target.sha256 && !target.markers?.length) fail(`No content assertion for ${target.name}`);
  }
  if (mode === 'hot') {
    // Warm both replicas through the public API before measuring sustained traffic.
    for (const base of bases) {
      for (const target of targets) {
        if (!request(base, target, false)) fail(`Warmup failed: ${target.name}`);
      }
    }
  }
  return { targets, before: fetchJSON('/__stats') as Stats };
}

export default function (data: Setup): void {
  const target = data.targets[(__VU + __ITER - 1) % data.targets.length];
  const base = bases[(__VU - 1) % bases.length];
  request(base, target);
}

export function teardown(data: Setup): void {
  const after = fetchJSON('/__stats') as Stats;
  const counts = data.targets.map(target => {
    const beforeCount = data.before.requests[target.upstream_path]?.GET || 0;
    const afterCount = after.requests[target.upstream_path]?.GET || 0;
    const delta = afterCount - beforeCount;
    // These streaming/manifest paths currently coalesce within each process.
    const processScoped = target.name === 'oci-manifest' || target.name === 'pypi-wheel';
    const coldLimit = processScoped ? bases.length : 1;
    upstreamFetches.add(delta, { ecosystem: target.ecosystem, target: target.name });
    check(delta, {
      'cold fill stays within owner count or hot traffic avoids upstream': count => mode === 'burst'
        ? count >= 1 && count <= coldLimit
        : count === 0,
    }, { ecosystem: target.ecosystem, target: target.name });
    return { name: target.name, upstreamGETs: delta, coldLimit };
  });
  console.log('UPSTREAM_COUNTS ' + JSON.stringify({ wave, mode, counts, before: data.before, after }));
}

export function handleSummary(data: unknown): Record<string, string> {
  return {
    [`/reports/${report}.json`]: JSON.stringify({
      configuration: { mode, wave, selectedName, concurrency, duration, bases, mixedMethods },
      summary: data,
    }, null, 2) + '\n',
  };
}
