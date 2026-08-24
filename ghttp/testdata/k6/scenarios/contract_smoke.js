import http from 'k6/http';
import { check, group } from 'k6';
import { baseURL, scenarioOptions } from '../lib/config.js';
import { checkJSON, checkProblem, tags } from '../lib/checks.js';
import { uniqueNamespace } from '../lib/data.js';

export const options = scenarioOptions('contract_smoke', ['secret_leaks', 'schema_success', 'route_success', 'route_duration']);

export default function () {
  const base = baseURL();
	const namespace = uniqueNamespace('contract');
  group('routing', () => checkJSON(http.get(`${base}/health`, { tags: tags('routing', 'health', 200) }), 200, (v) => v.status === 'ok'));
  group('binding', () => checkJSON(http.get(`${base}/binding/path/42`, { tags: tags('binding', 'path', 200) }), 200, (v) => v.id === 42));
  group('output', () => checkJSON(http.get(`${base}/output/json`, { tags: tags('output', 'json', 200) }), 200, (v) => v.message === 'hello'));
  group('error', () => checkProblem(http.get(`${base}/binding/path/not-an-int`, { tags: tags('error', 'bad_path', 400) }), 400));
  const output=http.batch([['GET',`${base}/output/json`],['GET',`${base}/output/text`],['GET',`${base}/output/binary`],['GET',`${base}/output/redirect`,null,{redirects:0}],['GET',`${base}/routes/static`]]);
  check(output, {
    'contract json status 200':r=>r[0].status===200, 'contract json content type':r=>(r[0].headers['Content-Type']||'').includes('application/json'), 'contract json source typed':r=>r[0].json().meta.source==='typed',
    'contract text status 200':r=>r[1].status===200, 'contract text content type':r=>(r[1].headers['Content-Type']||'').includes('text/plain'), 'contract text body nonempty':r=>r[1].body.length>0,
    'contract bytes status 200':r=>r[2].status===200, 'contract bytes body nonempty':r=>r[2].body.length>0,
    'contract redirect status':r=>r[3].status>=300&&r[3].status<400, 'contract redirect location present':r=>Boolean(r[3].headers.Location),
    'contract static status 200':r=>r[4].status===200, 'contract static route present':r=>r[4].json().route==='static',
    'contract request id json':r=>Boolean(r[0].headers['X-Request-Id']||r[0].headers['X-Request-ID']), 'contract request id text':r=>Boolean(r[1].headers['X-Request-Id']||r[1].headers['X-Request-ID']),
  });
  const partial=http.get(`${base}/files/range`,{headers:{Range:'bytes=0-3'}});
  const unsatisfied=http.get(`${base}/files/range`,{headers:{Range:'bytes=9999-'}});
  const etag=http.get(`${base}/files/etag`);
  const notModified=http.get(`${base}/files/etag`,{headers:{'If-None-Match':'"ghttp-k6-sample-v1"'}});
  const precondition=http.get(`${base}/files/etag`,{headers:{'If-Match':'"different"'}});
  const created=http.post(`${base}/output/created`,null,{redirects:0});
  const empty=http.del(`${base}/output/empty`);
  const headers=http.get(`${base}/output/headers`);
  check([partial,unsatisfied,etag,notModified,precondition,created,empty,headers], {
    'contract range prefix status 206':r=>r[0].status===206,
    'contract range prefix content range exact':r=>(r[0].headers['Content-Range']||'').startsWith('bytes 0-3/'),
    'contract range prefix body exact':r=>r[0].body==='ghtt',
    'contract range prefix content length 4':r=>r[0].headers['Content-Length']==='4',
    'contract range unsatisfied status 416':r=>r[1].status===416,
    'contract range unsatisfied content range':r=>(r[1].headers['Content-Range']||'').startsWith('bytes */'),
    'contract etag initial status 200':r=>r[2].status===200,
    'contract etag initial value exact':r=>Object.entries(r[2].headers).some(([k,v])=>k.toLowerCase()==='etag'&&String(v)==='"ghttp-k6-sample-v1"'),
    'contract if none match status 304':r=>r[3].status===304,
    'contract if none match body empty':r=>(r[3].body||'').length===0,
    'contract if match failure status 412':r=>r[4].status===412,
    'contract if match failure body empty':r=>(r[4].body||'').length===0,
    'contract created status 201':r=>r[5].status===201,
    'contract created location exact':r=>r[5].headers.Location==='/output/json',
    'contract no content status 204':r=>r[6].status===204,
    'contract no content body empty':r=>(r[6].body||'').length===0,
    'contract headers content length 5':r=>r[7].headers['Content-Length']==='5',
    'contract headers vary accept':r=>(r[7].headers.Vary||'').includes('Accept'),
  });
}