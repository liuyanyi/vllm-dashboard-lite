import urllib.request,json,time,sqlite3
base='http://127.0.0.1:9090/api'
def call(path,data=None,method=None):
 r=urllib.request.Request(base+path,data=json.dumps(data).encode() if data is not None else None,method=method,headers={'Content-Type':'application/json'})
 return json.load(urllib.request.urlopen(r))
e=call('/endpoints',{'url':'http://127.0.0.1:18000','record':True,'interval':500,'metrics_path':'/metrics-no-start'})
for i in range(40):
 s=call('/endpoints')[0]
 if s['model'] and s['latest'].get('values',{}).get('generation_tokens_total',0):break
 time.sleep(.5)
assert s['model']=='Qwen3.8-Flash-Next',s
assert not s['start'] and s['run']>0 and s['first_seen'],s
old=s['run'];assert s['latest']['values']['generation_tokens_total']>100
calls=json.load(urllib.request.urlopen('http://127.0.0.1:18000/debug'))['calls']
time.sleep(2)
assert json.load(urllib.request.urlopen('http://127.0.0.1:18000/debug'))['calls']==calls
urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:18000/restart',data=b''))
for i in range(40):
 s=call('/endpoints')[0]
 if s['run']!=old and s['model']=='GLM-5.3-Flash':break
 time.sleep(.5)
assert s['run']!=old and s['model']=='GLM-5.3-Flash',s
h=call('/history?run='+str(old));assert len(h)>0,h
runs=call('/runs?endpoint='+e['id']);assert len(runs)==2,runs
assert next(r for r in runs if r['id']==old)['model']=='Qwen3.8-Flash-Next'
scan=call('/scan',{'host':'127.0.0.1','ports':'18000-18001'});assert scan==['http://127.0.0.1:18000'],scan
call('/endpoints/'+e['id'],{'record':False},'PATCH')
print('PASS: live rates, throttled metadata, model session isolation without process timestamp, history flush, scan, recording toggle')
