import http from 'k6/http';

http.setResponseCallback(http.expectedStatuses(200, 201, 202, 204, 206, 304, 307, 400, 401, 403, 404, 405, 406, 409, 412, 413, 415, 416, 422, 428, 429, 500, 503, 504));

export function baseURL() {
  return (__ENV.BASE_URL || 'http://127.0.0.1:8080').replace(/\/$/, '');
}

export function thresholds(profile = __ENV.PROFILE || 'smoke', metrics = []) {
  const duration = profile === 'smoke' ? ['p(95)<1000'] : ['p(95)<500', 'p(99)<1500'];
  const result = {
    http_req_failed: ['rate<0.01'],
    http_req_duration: duration,
    checks: ['rate==1'],
  };
  const custom = { secret_leaks: ['count==0'], schema_success: ['rate==1'], route_success: ['rate==1'], route_duration: duration, auth_success: ['rate==1'], negotiation_success: ['rate==1'], openapi_success: ['rate==1'] };
  for (const metric of metrics) if (custom[metric]) result[metric] = custom[metric];
  return result;
}

export function scenarioOptions(name, metrics = ['secret_leaks', 'schema_success', 'route_success', 'route_duration']) {
  return {
    scenarios: { [name]: { executor: 'per-vu-iterations', vus: Number(__ENV.VUS || 1), iterations: Number(__ENV.ITERATIONS || 1), maxDuration: __ENV.MAX_DURATION || '30s' } },
    thresholds: thresholds(__ENV.PROFILE || 'smoke', metrics),
  };
}

export function executor(name, kind = __ENV.EXECUTOR || 'per-vu-iterations') {
  if (kind === 'constant-arrival-rate') return { executor: kind, rate: Number(__ENV.RATE || 5), timeUnit: '1s', duration: __ENV.DURATION || '30s', preAllocatedVUs: Number(__ENV.VUS || 5), maxVUs: Number(__ENV.MAX_VUS || 20), exec: name };
  if (kind === 'constant-vus') return { executor: kind, vus: Number(__ENV.VUS || 1), duration: __ENV.DURATION || '30s', exec: name };
  return { executor: kind, vus: Number(__ENV.VUS || 1), iterations: Number(__ENV.ITERATIONS || 1), maxDuration: __ENV.MAX_DURATION || '30s', exec: name };
}

export function soakDuration() { return __ENV.SOAK_DURATION || '90s'; }
export function streamTimeout() { return __ENV.STREAM_TIMEOUT || '10s'; }
