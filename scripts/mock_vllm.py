"""Local deterministic fixture. POST /restart changes process identity and model."""
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
import time,json,math
start=time.time(); generation=0; calls=0
class Handler(BaseHTTPRequestHandler):
 def do_POST(self):
  global start,generation
  start=time.time();generation+=1;self.send_response(200);self.end_headers();self.wfile.write(b'{}')
 def do_GET(self):
  global calls
  age=time.time()-start;model=['Qwen3.8-Flash-Next','GLM-5.3-Flash'][generation%2]
  if self.path=='/v1/models':
   calls+=1;body=json.dumps({'data':[{'id':model,'root':'/model','max_model_len':32768}]}).encode()
  elif self.path=='/debug':body=json.dumps({'calls':calls}).encode()
  elif self.path in ('/metrics','/metrics-no-start'):
   vals=[('process_start_time_seconds','gauge',start),('vllm:prompt_tokens_total','counter',int(age*2480)),('vllm:generation_tokens_total','counter',int(age*116)),('vllm:num_requests_running','gauge',8+int(2*math.sin(age/6))),('vllm:num_requests_waiting','gauge',2),('vllm:kv_cache_usage_perc','gauge',.426),('vllm:prefix_cache_hits_total','counter',int(age*68)),('vllm:prefix_cache_queries_total','counter',int(age*100)),('vllm:request_success_total','counter',int(age/2)),('vllm:spec_decode_num_draft_tokens_total','counter',int(age*100)),('vllm:spec_decode_num_accepted_tokens_total','counter',int(age*65)),('vllm:spec_decode_num_drafts_total','counter',int(age*33))]
   if self.path=='/metrics-no-start': vals=[v for v in vals if not v[0].startswith('process_')]
   body=''.join(f'# TYPE {n} {t}\n{n}'+('' if n.startswith('process_') else '{model_name="'+model+'",engine="0"}')+f' {v}\n' for n,t,v in vals).encode()
  else:self.send_response(404);self.end_headers();return
  self.send_response(200);self.end_headers();self.wfile.write(body)
 def log_message(self,*args):pass
ThreadingHTTPServer(('127.0.0.1',18000),Handler).serve_forever()
