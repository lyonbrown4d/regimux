import http from 'k6/http';
import crypto from 'k6/crypto';
import { check, fail } from 'k6';
import { Counter } from 'k6/metrics';

type Target = {
  name: string;
  ecosystem: string;
  path: string;
  upstream_path: string;
  status: number;
  sha256?: string;
  markers?: string[];
  wave: string;
};
type Fixtures = { targets: unknown[] };
type Stats = { requests: Record<string, { GET?: number; HEAD?: number }> };
type Setup = { targets: Target[]; before: Stats };

const fixtureURL = 'http://fixture:8081';
const baseURLs = (__ENV.BASE_URLS || 'http://regimux:8080,http://regimux-peer:8080').split(',');
const concurrency = Number(__ENV.DISTINCT_CONCURRENCY || '512');
const targetName = __ENV.DISTINCT_TARGET || 'go-zip';
const wavePrefix = __ENV.WAVE_PREFIX || 'memory-20261021';
const report = __ENV.REPORT || 'memory-distinct';
const setupBatchSize = 32;

if (!Number.isInteger(concurrency) || concurrency < 1 || concurrency > 2048) {
  throw new Error('DISTINCT_CONCURRENCY must be an integer between 1 and 2048');
}
if (baseURLs.length < 1 || baseURLs.some(url => !/^https?:\/\/(?:regimux|regimux-peer):8080$/.test(url))) {
  throw new Error('BASE_URLS must use the isolated RegiMux services');
}
if (!/^[a-z0-9][a-z0-9-]{0,42}$/.test(wavePrefix)) throw new Error('WAVE_PREFIX is invalid or too long');
if (!/^[a-z0-9-]+$/.test(targetName)) throw new Error('DISTINCT_TARGET is invalid');
if (!/^[a-z0-9._-]+$/i.test(report)) throw new Error('REPORT must be a filename');

const corruptResponses = new Counter('distinct_corrupt_responses');
const upstreamFetches = new Counter('distinct_upstream_fetches');

export const options = {
  scenarios: {
    distinct: { executor: 'per-vu-iterations', vus: concurrency, iterations: 1, maxDuration: '180s' },
  },
  thresholds: {
    http_req_failed: ['rate==0'],
    checks: ['rate==1'],
    distinct_corrupt_responses: ['count==0'],
  },
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  systemTags: ['status', 'method', 'name', 'scenario', 'expected_response'],
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function parseTarget(value: unknown, wave: string): Target {
  if (!isRecord(value)
    || typeof value.name !== 'string'
    || typeof value.ecosystem !== 'string'
    || typeof value.path !== 'string'
    || typeof value.upstream_path !== 'string'
    || typeof value.status !== 'number') {
    fail(`Fixture returned an invalid target record for ${wave}`);
  }
  const sha256 = typeof value.sha256 === 'string' ? value.sha256 : undefined;
  const markers = Array.isArray(value.markers) && value.markers.every(item => typeof item === 'string')
    ? value.markers as string[]
    : undefined;
  if ((!sha256 && !markers?.length) || !value.path.startsWith('/') || value.path.startsWith('//')) {
    fail(`Fixture target lacks a valid body assertion: ${wave}`);
  }
  return {
    name: value.name,
    ecosystem: value.ecosystem,
    path: value.path,
    upstream_path: value.upstream_path,
    status: value.status,
    sha256,
    markers,
    wave,
  };
}

function readTarget(wave: string, response: http.Response): Target {
  if (response.status !== 200) fail(`Fixture metadata failed for ${wave}: ${response.status}`);
  const payload = response.json() as unknown;
  if (!isRecord(payload) || !Array.isArray((payload as Fixtures).targets)) {
    fail(`Fixture metadata was malformed for ${wave}`);
  }
  const selected = (payload as Fixtures).targets.find(value => isRecord(value) && value.name === targetName);
  if (selected === undefined) fail(`Fixture has no ${targetName} target for ${wave}`);
  return parseTarget(selected, wave);
}

function readStats(): Stats {
  const response = http.get(`${fixtureURL}/__stats`, { timeout: '15s', tags: { name: 'fixture-stats' } });
  if (response.status !== 200) fail(`Fixture stats failed: ${response.status}`);
  const value = response.json() as unknown;
  if (!isRecord(value) || !isRecord(value.requests)) fail('Fixture stats were malformed');
  return value as Stats;
}

export function setup(): Setup {
  const targets: Target[] = [];
  for (let start = 1; start <= concurrency; start += setupBatchSize) {
    const waves: string[] = [];
    const requests: string[] = [];
    for (let vu = start; vu < Math.min(start + setupBatchSize, concurrency + 1); vu += 1) {
      const wave = `${wavePrefix}-${String(vu).padStart(4, '0')}`;
      waves.push(wave);
      requests.push(`${fixtureURL}/__fixtures?wave=${wave}`);
    }
    const responses = http.batch(requests);
    for (let index = 0; index < responses.length; index += 1) {
      targets.push(readTarget(waves[index], responses[index]));
    }
  }
  return { targets, before: readStats() };
}

export default function (data: Setup): void {
  const target = data.targets[__VU - 1];
  if (!target) fail(`No distinct-key target record for VU ${__VU}`);
  const baseURL = baseURLs[(__VU - 1) % baseURLs.length];
  const response = http.get(baseURL + target.path, {
    timeout: '90s',
    responseType: target.sha256 ? 'binary' : 'text',
    tags: { name: target.name, ecosystem: target.ecosystem, wave: target.wave },
  });
  const body = response.body;
  const bodyOK = target.sha256
    ? body !== null && crypto.sha256(body, 'hex') === target.sha256
    : typeof body === 'string'
      && (target.markers || []).every(marker => body.includes(marker))
      && !body.includes('http://fixture:8081')
      && !body.includes('https://fixture.invalid');
  const ok = check(response, {
    'expected status': value => value.status === target.status,
    'complete validated fixture body': () => bodyOK,
  }, { ecosystem: target.ecosystem, target: target.name, wave: target.wave });
  corruptResponses.add(bodyOK ? 0 : 1, { ecosystem: target.ecosystem, target: target.name });
  if (!ok) {
    console.error(JSON.stringify({
      target: target.name,
      wave: target.wave,
      status: response.status,
      bodyOK,
      error: response.error,
      errorCode: response.error_code,
    }));
  }
}

export function teardown(data: Setup): void {
  const after = readStats();
  const counts = data.targets.map(target => {
    const before = data.before.requests[target.upstream_path]?.GET || 0;
    const current = after.requests[target.upstream_path]?.GET || 0;
    const delta = current - before;
    upstreamFetches.add(delta, { ecosystem: target.ecosystem, target: target.name });
    const once = check(delta, {
      'distinct key fetched exactly once from fixture': value => value === 1,
    }, { ecosystem: target.ecosystem, target: target.name, wave: target.wave });
    if (!once) console.error(`upstream GETs for ${target.wave}: ${delta}, want 1`);
    return { wave: target.wave, target: target.name, upstreamGETs: delta };
  });
  console.log(`DISTINCT_UPSTREAM_COUNTS ${JSON.stringify({ targetName, concurrency, counts })}`);
}

export function handleSummary(data: unknown): Record<string, string> {
  return {
    [`/reports/${report}.json`]: JSON.stringify({
      configuration: { targetName, concurrency, wavePrefix, baseURLs },
      summary: data,
    }, null, 2) + '\n',
  };
}
