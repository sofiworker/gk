import http from 'k6/http';
import { check } from 'k6';
import { baseURL, scenarioOptions } from '../lib/config.js';
import { checkJSON, checkProblem, tags } from '../lib/checks.js';
import { uniqueNamespace } from '../lib/data.js';
import { negotiationSuccess } from '../lib/metrics.js';

export const options = scenarioOptions('binding_codec', ['secret_leaks', 'schema_success', 'route_success', 'route_duration', 'negotiation_success']);
export default function () {
  const base = baseURL();
	const namespace = uniqueNamespace('binding');
  checkJSON(http.get(`${base}/binding/query?name=k6&count=7&enabled=true&tag=a&tag=b`, { tags: tags('binding', 'query', 200) }), 200, (v) => v.name === 'k6');
  checkJSON(http.post(`${base}/binding/mixed/8?q=hello`, JSON.stringify({ name: 'world' }), { headers: { 'Content-Type': 'application/json', 'X-Trace-ID': 'trace-8', 'X-Test-Namespace': namespace }, tags: tags('binding', 'mixed', 200) }), 200, (v) => v.id === 8 && v.q === 'hello');
  const negotiated = http.post(`${base}/codec/negotiate`, JSON.stringify({ name: 'x' }), { headers: { 'Content-Type': 'application/json', Accept: 'text/html' }, tags: tags('codec', 'not_acceptable', 406) });
  checkProblem(negotiated, 406);
  negotiationSuccess.add(negotiated.status === 406, negotiated.tags);
  const form=http.post(`${base}/codec/form`,'name=form&count=3',{headers:{'Content-Type':'application/x-www-form-urlencoded'}});
  const xml=http.post(`${base}/codec/xml`,'<input><name>xml</name></input>',{headers:{'Content-Type':'application/xml'}});
  const header=http.get(`${base}/binding/header`,{headers:{'X-Trace-ID':'trace-k6','X-Count':'9'}});
  const cookie=http.get(`${base}/binding/cookie`,{headers:{Cookie:'session=s-123'}});
  const path=http.get(`${base}/binding/path/42`);
  const query=http.get(`${base}/binding/query?name=k6&count=7&enabled=true&tag=a&tag=b`);
  check([path,query,header,cookie,form,xml,negotiated], {
    'binding path status 200':r=>r[0].status===200, 'binding path integer decoded':r=>r[0].json().id===42,
    'binding query status 200':r=>r[1].status===200, 'binding query string decoded':r=>r[1].json().name==='k6', 'binding query integer decoded':r=>r[1].json().count===7, 'binding query boolean decoded':r=>r[1].json().enabled===true, 'binding query repeated tags decoded':r=>r[1].json().tags.length===2,
    'binding header status 200':r=>r[2].status===200, 'binding header trace decoded':r=>r[2].json().trace_id==='trace-k6', 'binding header integer decoded':r=>r[2].json().count===9,
    'binding cookie status 200':r=>r[3].status===200, 'binding cookie session decoded':r=>r[3].json().session==='s-123',
    'codec form accepted':r=>r[4].status===200, 'codec form response json':r=>(r[4].headers['Content-Type']||'').includes('application/json'),
    'codec xml accepted':r=>r[5].status===200, 'codec xml response content type':r=>(r[5].headers['Content-Type']||'').includes('xml'),
    'codec negotiation rejects html':r=>r[6].status===406, 'codec negotiation problem type':r=>(r[6].headers['Content-Type']||'').includes('application/problem+json'),
  });
}
