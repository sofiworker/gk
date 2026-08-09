import http from 'k6/http';
import { check } from 'k6';
import { baseURL, scenarioOptions } from '../lib/config.js';
import { checkJSON, tags } from '../lib/checks.js';
import { uniqueNamespace } from '../lib/data.js';

export const options = scenarioOptions('routing_matrix');
const routes = ['/routes/static', '/routes/users/new', '/routes/users/42', '/routes/pairs/left/right', '/routes/files/css/app.css', '/routes/groups/v1/items/7'];
export default function () {
  const base = baseURL();
  const namespace = uniqueNamespace('routing');
  for (const path of routes) checkJSON(http.get(`${base}${path}`, { headers: { 'X-Test-Namespace': namespace }, tags: tags('routing', path, 200) }), 200, (v) => v && typeof v === 'object');
  const rs=http.batch([['GET',`${base}/routes/static`],['GET',`${base}/routes/users/new`],['GET',`${base}/routes/users/42`],['GET',`${base}/routes/pairs/left/right`],['GET',`${base}/routes/files/css/app.css`],['GET',`${base}/routes/groups/v1/items/7`],['HEAD',`${base}/health`],['POST',`${base}/routes/method`],['DELETE',`${base}/routes/method`],['PUT',`${base}/routes/method`]]);
  check(rs, {
    'routing static status 200': r=>r[0].status===200, 'routing static body marker': r=>r[0].json().route==='static',
    'routing static precedence status 200': r=>r[1].status===200, 'routing static precedence id new': r=>r[1].json().id==='new',
    'routing parameter status 200': r=>r[2].status===200, 'routing parameter id 42': r=>r[2].json().id==='42',
    'routing pair status 200': r=>r[3].status===200, 'routing pair captures both': r=>r[3].json().left==='left'&&r[3].json().right==='right',
    'routing wildcard status 200': r=>r[4].status===200, 'routing wildcard preserves path': r=>r[4].json().path==='css/app.css',
    'routing group status 200': r=>r[5].status===200, 'routing group captures id': r=>r[5].json().id==='7',
    'routing HEAD status 200': r=>r[6].status===200, 'routing HEAD body empty': r=>r[6].body.length===0,
    'routing POST method 204': r=>r[7].status===204, 'routing DELETE method 204': r=>r[8].status===204,
    'routing unsupported method 405': r=>r[9].status===405, 'routing unsupported allow header': r=>(r[9].headers.Allow||'').includes('POST'),
  });
}
