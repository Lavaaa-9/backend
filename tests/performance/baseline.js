import http from 'k6/http';
import {check,sleep} from 'k6';
import {Trend} from 'k6/metrics';
const loginDuration=new Trend('login_duration',true);
const searchDuration=new Trend('search_duration',true);
const registerDuration=new Trend('register_duration',true);
const base=__ENV.API_BASE_URL||'http://127.0.0.1:8080';
export const options={scenarios:{login:{executor:'shared-iterations',exec:'loginProbe',vus:1,iterations:2,maxDuration:'15s'},registration:{executor:'shared-iterations',exec:'register',vus:2,iterations:20,maxDuration:'30s'},search:{executor:'constant-vus',exec:'search',vus:2,duration:'5s'}},thresholds:{http_req_failed:['rate<0.01']}};
function login(){return http.post(base+'/api/v1/auth/login',JSON.stringify({email:'reception@spartial.test',password:__ENV.SEED_RECEPTION_PASSWORD,client_type:'mobile',device_label:'Synthetic performance probe'}),{headers:{'Content-Type':'application/json'},tags:{name:'staff_login'}})}
export function setup(){if(__ENV.APP_ENV!=='development')throw new Error('Synthetic development environment required');const r=login();if(!check(r,{'setup login succeeded':v=>v.status===200}))throw new Error('Login unavailable; respect durable authentication rate limits before rerunning');const body=r.json();return {token:body.access_token,facility:body.user.facility_ids[0],run:String(Date.now())}}
export function loginProbe(){const r=login();loginDuration.add(r.timings.duration);check(r,{'login succeeded':v=>v.status===200});if(r.status===200)http.post(base+'/api/v1/auth/logout',null,{headers:{Authorization:'Bearer '+r.json().access_token},tags:{name:'logout'}})}
export function register(data){const r=http.post(base+'/api/v1/staff/patients',JSON.stringify({facility_id:data.facility,given_name:'SyntheticBench',family_name:'Patient',birth_precision:'unknown'}),{headers:{'Content-Type':'application/json',Authorization:'Bearer '+data.token,'Idempotency-Key':`benchmark-${data.run}-${__VU}-${__ITER}`},tags:{name:'register_patient'}});registerDuration.add(r.timings.duration);check(r,{'registration succeeded':v=>v.status===201});sleep(0.05)}
export function search(data){const r=http.get(base+'/api/v1/staff/patients?q=SyntheticBench&limit=25',{headers:{Authorization:'Bearer '+data.token},tags:{name:'search_patients'}});searchDuration.add(r.timings.duration);check(r,{'search succeeded':v=>v.status===200});sleep(0.1)}
export function teardown(data){http.post(base+'/api/v1/auth/logout',null,{headers:{Authorization:'Bearer '+data.token},tags:{name:'logout'}})}
export function handleSummary(data){const out={};out[__ENV.K6_SUMMARY_FILE||'.local/performance.json']=JSON.stringify({state:data.state,metrics:data.metrics,root_group:data.root_group},null,2);out.stdout='Synthetic login/search/registration baseline completed; aggregate metrics saved without credentials or patient bodies.\n';return out}
