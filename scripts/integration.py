import urllib.request, urllib.error, json, time
base='http://127.0.0.1:9090/api'
def call(path,data=None,method=None):
 r=urllib.request.Request(base+path,data=json.dumps(data).encode() if data is not None else None,method=method,headers={'Content-Type':'application/json'})
 return json.load(urllib.request.urlopen(r))
def wait(pred):
 for _ in range(80):
  s=call('/endpoints')[0]
  if pred(s):return s
  time.sleep(.5)
 raise AssertionError(s)
s=wait(lambda s:s['status']=='online' and (s['latest'].get('values') or {}).get('generation_tokens_total'))
assert s['model']=='configured-model' and s['config']['id']=='mock-generation',s
assert s['start'] and s['run']>0 and s['first_seen'],s
old=s['run'];assert s['latest']['values']['generation_tokens_total']>100
urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:18000/restart',data=b''))
s=wait(lambda s:s['run']!=old and s['reason']=='process_changed')
assert s['model']=='configured-model',s
h=call('/history?run='+str(old));assert len(h)>0,h
runs=call('/runs?endpoint=mock-generation');assert next(r for r in runs if r['id']==old)['model']=='configured-model'
assert json.load(urllib.request.urlopen('http://127.0.0.1:18000/debug'))['calls']==0
for path,method in [('/scan','POST'),('/endpoints','POST'),('/endpoints/mock-generation','PATCH'),('/endpoints/mock-generation','DELETE')]:
 try:
  call(path,{},method)
 except urllib.error.HTTPError as e:
  assert e.code in (404,405),e
 else:raise AssertionError('removed endpoint API still exists: '+path)
print('PASS: YAML-only endpoints, live rates, process restart, no discovery, history, removed mutation APIs')
