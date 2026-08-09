let sequence = 0;

export function uniqueNamespace(prefix = 'k6') {
  sequence += 1;
  return `${prefix}-vu${__VU}-iter${__ITER}-${sequence}`;
}

export function stateHeaders(namespace) { return { 'X-Test-Namespace': namespace, 'Content-Type': 'application/json' }; }
export function securityHeaders(token = __ENV.AUTH_TOKEN || '') { return token ? { Authorization: `Bearer ${token}` } : {}; }
