import { check } from 'k6';
import { routeDuration, routeSuccess, schemaSuccess, secretLeaks } from './metrics.js';

export function tags(routeFamily, operation, expectedStatus) {
  return { route_family: routeFamily, operation, expected_status: String(expectedStatus) };
}

export function checkJSON(res, expectedStatus, predicate = () => true, secret = __ENV.SECRET || 'known-k6-secret') {
  let body;
  try { body = res.json(); } catch (_) { body = null; }
  const schemaOK = body !== null && Boolean(predicate(body));
  const ok = check(res, {
    [`status is ${expectedStatus}`]: (r) => r.status === expectedStatus,
    'content type is json': (r) => {
      const contentType = r.headers['Content-Type'] || '';
      return contentType.includes('application/json') || contentType.includes('application/problem+json');
    },
    'request id present': (r) => Boolean(r.headers['X-Request-Id'] || r.headers['X-Request-ID']),
    'json schema predicate': () => schemaOK,
    'secret absent': (r) => !r.body.includes(secret),
  });
  routeDuration.add(res.timings.duration, res.tags);
  routeSuccess.add(ok, res.tags);
  schemaSuccess.add(schemaOK, res.tags);
  const leaked = res.body.includes(secret);
  secretLeaks.add(leaked ? 1 : 0, res.tags);
  return body;
}

export function checkProblem(res, expectedStatus) {
  const typeOK = (res.headers['Content-Type'] || '').includes('application/problem+json');
  const value = checkJSON(res, expectedStatus, (v) => v && v.status === expectedStatus && typeof v.title === 'string' && typeof v.type === 'string');
  check(res, { 'content type is problem json': () => typeOK });
  return value;
}
