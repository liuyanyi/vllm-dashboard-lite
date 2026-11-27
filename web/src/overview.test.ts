import {describe,expect,it} from 'vitest';
import {overviewRows,summarize} from './overview';
import type {Endpoint} from './store';
const now=100000;
function instance(id:string,backend:'tei'|'vllm',task:'generation'|'embedding'|'reranker',values:Record<string,number|null>,status='online'):Endpoint{
  return {config:{id,url:'http://'+id,name:'same-name',model:'same-model',record:false,interval:1000,backend,task},status,run:1,start:'',model:'same-model',latest:{at:now,values},points:[]};
}
describe('overview',()=>{
  it('keeps same-name replicas distinct and preserves backend units',()=>{
    const rows=overviewRows([instance('a','tei','embedding',{te_input_tokens_rate:100,te_embed_success_rate:2}),instance('b','vllm','reranker',{prompt_tokens_total:200,request_success_total:4,pooling_api_request_rate:1})],true,now);
    expect(rows.map(r=>r.e.config.id)).toEqual(['a','b']);
    expect(rows.map(r=>r.completedUnit)).toEqual(['texts/s','engine/s']);
    expect(rows[1].api).toBe(1);
    expect(summarize(rows,'input')).toEqual({value:300,known:2,total:2});
  });
  it('excludes offline, stale and absent samples but preserves a real zero',()=>{
    const rows=overviewRows([instance('a','tei','embedding',{te_input_tokens_rate:0}),instance('b','tei','embedding',{te_input_tokens_rate:900},'offline'),{...instance('c','vllm','generation',{prompt_tokens_total:1000}),latest:{at:now-30000,values:{prompt_tokens_total:1000}}}],true,now);
    expect(summarize(rows,'input')).toEqual({value:0,known:1,total:3});
    expect(summarize(overviewRows(rows.map(r=>r.e),false,now),'input')).toEqual({value:null,known:0,total:3});
  });
});
