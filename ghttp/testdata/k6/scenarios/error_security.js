import http from 'k6/http';
import { baseURL, scenarioOptions } from '../lib/config.js';
import { checkProblem, tags } from '../lib/checks.js';
import { authSuccess } from '../lib/metrics.js';
import { uniqueNamespace } from '../lib/data.js';
import { check } from 'k6';

export const options = scenarioOptions('error_security', ['secret_leaks', 'schema_success', 'route_success', 'route_duration', 'auth_success']);
export default function () {
  const base = baseURL();
	const namespace = uniqueNamespace('security');
  checkProblem(http.get(`${base}/binding/path/0`, { tags: tags('error', 'validation', 422) }), 422);
  const unauthorized = http.get(`${base}/auth/user`, { tags: tags('security', 'unauthorized', 401) });
  authSuccess.add(unauthorized.status === 401, unauthorized.tags);
  checkProblem(unauthorized, 401);
  checkProblem(http.get(`${base}/fault/panic`, { headers: { 'X-Test-Namespace': namespace }, tags: tags('error', 'panic_recovery', 500) }), 500);
  const recovered = http.get(`${base}/health`, { tags: tags('security', 'post_error_health', 200) });
  check(recovered, { 'service recovers after error': (r) => r.status === 200 });
  const rs=http.batch([['GET',`${base}/__missing__`],['GET',`${base}/binding/path/not-an-int`],['GET',`${base}/binding/path/0`],['POST',`${base}/codec/negotiate`,JSON.stringify({name:'x'}),{headers:{'Content-Type':'application/json',Accept:'text/html'}}],['POST',`${base}/codec/xml`,JSON.stringify({name:'x'}),{headers:{'Content-Type':'application/json'}}],['GET',`${base}/fault/panic`],['GET',`${base}/health`]]);
  const secret=__ENV.SECRET||'known-k6-secret';
  check(rs, {
    'security not found status 404':r=>r[0].status===404, 'security not found problem json':r=>(r[0].headers['Content-Type']||'').includes('application/problem+json'),
    'security bad path status 400':r=>r[1].status===400, 'security bad path has title':r=>typeof r[1].json().title==='string',
    'security validation status 422':r=>r[2].status===422, 'security validation reports status':r=>r[2].json().status===422,
    'security negotiation status 406':r=>r[3].status===406, 'security negotiation problem json':r=>(r[3].headers['Content-Type']||'').includes('application/problem+json'),
    'security media type status 415':r=>r[4].status===415, 'security media type problem status':r=>r[4].json().status===415,
    'security panic status 500':r=>r[5].status===500, 'security panic body hides secret':r=>!r[5].body.includes(secret), 'security panic headers hide secret':r=>!JSON.stringify(r[5].headers).includes(secret),
    'security recovery health status 200':r=>r[6].status===200, 'security recovery health body ok':r=>r[6].json().status==='ok',
    'security all errors carry request id':r=>r.slice(0,6).every(x=>Boolean(x.headers['X-Request-Id']||x.headers['X-Request-ID'])),
    'security all problem bodies hide secret':r=>r.slice(0,6).every(x=>!x.body.includes(secret)),
  });
}
