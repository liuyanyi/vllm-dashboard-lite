import {Chart} from './chart';
import type {Point} from './store';

type Values=Record<string,number|null>;
type Metric=[label:string,key:string,unit:string,scale?:number];
const format=(n:number|null|undefined,scale=1)=>n==null?'—':(n*scale).toLocaleString('en-US',{maximumFractionDigits:1});

export function RetrievalView({backend,task,points,values}:{backend:string;task:string;points:Point[];values:Values}){
  const tei=backend==='tei';
  const sample=task==='reranker'?'te_predict':'te_embed';
  const sampleUnit=task==='reranker'?'pairs/s':'texts/s';
  const latency=tei?'te_request_duration_seconds':'pooling_api_latency_seconds';
  const tokens=tei?'te_input_tokens_rate':'prompt_tokens_total';
  const api=tei?'te_request_count_rate':'pooling_api_request_rate';
  const samples=tei?sample+'_success_rate':'request_success_total';
  const metrics:Metric[]=[
    ['Input tokens',tokens,'tok/s'],
    ['API requests',api,'req/s'],
    [tei?'Completed samples':'Engine completions',samples,tei?sampleUnit:'items/s'],
    ['Waiting',tei?'te_queue_size':'num_requests_waiting',tei?'items':'engine req'],
    [tei?'Inference / input':'Inference / engine req',tei?sample+'_inference_duration_seconds':'request_inference_time_seconds','ms',1000],
    ['API P95',latency+'_p95','ms',1000]
  ];
  return <>
    <section className="metrics">{metrics.map(([label,key,unit,scale])=><div key={key}><span>{label}</span><p>{format(values[key],scale)}<small>{unit}</small></p></div>)}</section>
    <div className="grid">
      <section className="panel"><h3>输入吞吐</h3><Chart points={points} unit="tok/s" series={[{key:tokens,name:'Input tokens',color:'#2563eb'}]}/></section>
      <section className="panel"><h3>完成吞吐 · {tei?sampleUnit:'engine items/s'}</h3><Chart points={points} unit={tei?sampleUnit:'items/s'} series={[{key:samples,name:tei?'完成样本':'Engine completions',color:'#10b981'}]}/></section>
      <section className="panel"><h3>API 请求吞吐</h3><Chart points={points} unit="req/s" series={[{key:api,name:'Requests',color:'#2563eb'},...(tei?[{key:'te_request_success_rate',name:'Successful requests',color:'#10b981'}]:[])]}/></section>
      <section className="panel"><h3>API 延迟</h3><Chart points={points} unit="ms" series={[{key:latency,name:'Mean',color:'#2563eb',scale:1000},{key:latency+'_p95',name:'P95',color:'#f59e0b',scale:1000}]}/></section>
      <section className="panel"><h3>{tei?'每个输入的阶段耗时':'引擎阶段耗时'}</h3><Chart points={points} unit="ms" series={tei?[{key:sample+'_tokenization_duration_seconds',name:'Tokenization',color:'#a855f7',scale:1000},{key:sample+'_queue_duration_seconds',name:'Queue',color:'#f59e0b',scale:1000},{key:sample+'_inference_duration_seconds',name:'Inference',color:'#2563eb',scale:1000}]:[{key:'request_queue_time_seconds',name:'Queue',color:'#f59e0b',scale:1000},{key:'request_inference_time_seconds',name:'Inference',color:'#2563eb',scale:1000},{key:'request_prefill_time_seconds',name:'Prefill',color:'#10b981',scale:1000}]}/></section>
      <section className="panel"><h3>{tei?'等待输入':'引擎请求'}</h3><Chart points={points} unit={tei?'items':'engine req'} series={tei?[{key:'te_queue_size',name:'Waiting inputs',color:'#f59e0b'}]:[{key:'num_requests_running',name:'Running',color:'#2563eb'},{key:'num_requests_waiting',name:'Waiting',color:'#f59e0b'}]}/></section>
      {tei&&<>
        <section className="panel"><h3>每次调度轮询的平均输入数（含空轮询）</h3><Chart points={points} unit="items" series={[{key:'te_batch_size_mean',name:'Mean inputs / poll',color:'#2563eb'}]}/></section>
        <section className="panel"><h3>每次调度轮询的平均 Token 数（含空轮询）</h3><Chart points={points} unit="tokens" series={[{key:'te_batch_tokens_mean',name:'Mean tokens / poll',color:'#10b981'}]}/></section>
      </>}
    </div>
    <p className="retrieval-note">{tei?'API 请求与输入样本分别统计；Token 吞吐为截断后的分词 Token 数。':'API 请求和引擎请求分别统计；Pooling 指标是否提供取决于 vLLM 版本。'} 未提供或尚无有效样本的指标显示为 —。</p>
  </>;
}
