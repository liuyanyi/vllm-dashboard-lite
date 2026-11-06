// Run after npm run prepare:backend. No desktop display required.
const {startBackend}=require('./backend.cjs');const path=require('node:path');const fs=require('node:fs');const os=require('node:os');
(async()=>{const dir=fs.mkdtempSync(path.join(os.tmpdir(),'monitor-desktop-'));let b;
try{b=startBackend(path.join(__dirname,'bin',process.platform==='win32'?'vllm-monitor.exe':'vllm-monitor'),path.join(dir,'monitor.db'));const url=await b.ready;const r=await fetch(url+'/api/endpoints');if(!r.ok)throw Error('backend not reachable');await b.stop();if(b.child.exitCode!==0)throw Error('unclean exit');console.log('PASS real Go desktop child: random port, API, graceful exit')}finally{if(b)await b.stop();fs.rmSync(dir,{recursive:true,force:true})}})().catch(e=>{console.error(e);process.exit(1)});
