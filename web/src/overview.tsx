import {Activity,ArrowRight,Database,Layers,Server} from 'lucide-react';
import type {Endpoint} from './store';
type Values=Record<string,number|null>;
const fmt=(value:number|null|undefined,scale=1)=>value==null?'—':(value*scale).toLocaleString('en-US',{maximumFractionDigits:1});
function fresh(e:Endpoint,connected:boolean,now:number):Values {
  const maxAge=Math.max(12000,e.config.interval*4+4000);
  return connected&&e.status==='online'&&e.latest?.at&&now-e.latest.at<=maxAge?e.latest.values:{};
}
type Row={e:Endpoint;input:number|null|undefined;output:number|null|undefined;api:number|null|undefined;completed:number|null|undefined;completedUnit:string;waiting:number|null|undefined;waitingUnit:string;p95:number|null|undefined;latencyUnit:string};
export function overviewRows(items:Endpoint[],connected:boolean,now=Date.now()):Row[]{
  return items.map(e=>{
    const v=fresh(e,connected,now);
    const tei=e.config.backend==='tei';
    const gen=!tei&&(!e.config.task||e.config.task==='generation');
    const sample=e.config.task==='reranker'?'te_predict':'te_embed';
    return {e,input:v[tei?'te_input_tokens_rate':'prompt_tokens_total'],output:gen?v.generation_tokens_total:null,api:v[tei?'te_request_count_rate':'pooling_api_request_rate'],completed:v[tei?sample+'_success_rate':'request_success_total'],completedUnit:tei?(e.config.task==='reranker'?'pairs/s':'texts/s'):'engine/s',waiting:v[tei?'te_queue_size':'num_requests_waiting'],waitingUnit:tei?'items':'engine req',p95:v[tei?'te_request_duration_seconds_p95':gen?'e2e_request_latency_seconds_p95':'pooling_api_latency_seconds_p95'],latencyUnit:gen?'engine':'API'};
  });
}
export function summarize(rows:Row[],key:'input'|'output'){
  const known=rows.filter(r=>r[key]!=null);
  return {value:known.length?known.reduce((n,r)=>n+r[key]!,0):null,known:known.length,total:rows.length};
}
export function Overview({items,connected,onSelect}:{items:Endpoint[];connected:boolean;onSelect:(id:string)=>void}){
  const rows=overviewRows(items,connected);
  const generation=rows.filter(r=>r.e.config.task==='generation'||(!r.e.config.task&&r.e.config.backend!=='tei'));
  const retrieval=rows.filter(r=>!generation.includes(r));
  const online=items.filter(e=>e.status==='online').length;
  const connecting=items.filter(e=>e.status==='connecting').length;
  const offline=items.filter(e=>e.status==='offline').length;
  const inputGen=summarize(generation,'input'),outputGen=summarize(generation,'output'),inputRetrieval=summarize(retrieval,'input');
  const groups=[{title:'Generation',rows:generation},{title:'Embedding / Reranker',rows:retrieval}];
  return <div className="content overview">
    {!connected&&<div className="notice">采集器连接尚未建立或已断开，实时指标暂不显示。</div>}
    <div className="overview-heading"><div><h2>推理服务总览</h2><p>仅展示 YAML 中声明的独立实例 · 点击实例进入详细监控</p></div><span><Activity size={15}/> {connected?'实时':'等待连接'}</span></div>
    <div className="overview-kpis">
      <section><span><Server size={15}/> 服务实例</span><strong>{items.length}</strong><small>{online} 在线 · {connecting} 连接中 · {offline} 离线{!connected?'（上次状态）':''}</small></section>
      <section><span><Layers size={15}/> 生成输入吞吐</span><strong>{fmt(inputGen.value)} <small>tok/s</small></strong><small>{inputGen.known}/{inputGen.total} 实例有数据</small></section>
      <section><span><Activity size={15}/> 生成输出吞吐</span><strong>{fmt(outputGen.value)} <small>tok/s</small></strong><small>{outputGen.known}/{outputGen.total} 实例有数据</small></section>
      <section><span><Database size={15}/> 检索输入吞吐</span><strong>{fmt(inputRetrieval.value)} <small>tok/s</small></strong><small>{inputRetrieval.known}/{inputRetrieval.total} 实例有数据</small></section>
    </div>
    {!items.length&&<section className="panel overview-empty"><h3>暂无配置实例</h3><p>编辑 monitor.yaml 的 endpoints 列表，然后重启采集器。</p></section>}
    {groups.filter(g=>g.rows.length).map(g=><section className="panel overview-group" key={g.title}>
      <h3>{g.title} <small>{g.rows.length} instances</small></h3>
      <div className="overview-table"><table><thead><tr><th>服务 / 模型</th><th>状态</th><th>Input tok/s</th>{g.title==='Generation'&&<th>Output tok/s</th>}{g.title!=='Generation'&&<th>API req/s</th>}<th>完成项/s</th><th>等待</th><th>P95 延迟</th><th>详情</th></tr></thead><tbody>
      {g.rows.map(r=><tr key={r.e.config.id}><td><button className="overview-name" onClick={()=>onSelect(r.e.config.id)}>{r.e.config.name||r.e.model||r.e.config.model}</button><small>{r.e.config.id} · {(r.e.config.backend||'vllm').toUpperCase()} · {r.e.config.task||'generation'}</small><small>{r.e.config.model||r.e.model}</small></td><td><span className="overview-status"><i className={'dot '+(connected?r.e.status:'offline')}/>{connected?(({online:'在线',offline:'离线',connecting:'连接中'} as Record<string,string>)[r.e.status]||r.e.status):'未知'}</span>{connected&&r.e.error&&<small className="overview-error">{r.e.error}</small>}</td><td>{fmt(r.input)}</td>{g.title==='Generation'&&<td>{fmt(r.output)}</td>}{g.title!=='Generation'&&<td>{fmt(r.api)}</td>}<td>{fmt(r.completed)}<small>{r.completedUnit}</small></td><td>{fmt(r.waiting)}<small>{r.waitingUnit}</small></td><td>{fmt(r.p95,1000)}<small>ms · {r.latencyUnit}</small></td><td><button className="overview-detail" aria-label={'查看 '+r.e.config.id} onClick={()=>onSelect(r.e.config.id)}><ArrowRight size={16}/></button></td></tr>)}
      </tbody></table></div>
    </section>)}
    <p className="retrieval-note">吞吐总计仅汇总有有效实时样本的实例，生成与检索分开统计；不同后端的 Token 口径可能不同。完成项与等待量按各后端原始单位展示，P95 不跨实例平均。缺失或陈旧数据为 —。</p>
  </div>;
}
