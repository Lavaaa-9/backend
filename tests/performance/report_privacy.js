import {check} from 'k6';
import {handleSummary} from './baseline.js';
export const options={vus:1,iterations:1,thresholds:{checks:['rate==1']}};
export default function(){
 const result=handleSummary({state:{},metrics:{},root_group:{},setup_data:{token:'demo'},options:{environment:'demo2'}});
 const serialized=result[__ENV.K6_SUMMARY_FILE||'.local/performance.json'];
 check(serialized,{'report excludes setup credentials and options':v=>!v.includes('demo')&&!v.includes('setup_data')&&!v.includes('options')});
}
