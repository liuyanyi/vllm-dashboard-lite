const {spawn}=require('node:child_process');
const {createInterface}=require('node:readline');
function startBackend(binary,db,{timeout=15000,spawnFn=spawn}={}) {
 const child=spawnFn(binary,['-listen','127.0.0.1:0','-db',db,'-desktop-child'],{stdio:['pipe','pipe','pipe'],windowsHide:true});
 let stderr='';child.stderr.on('data',b=>{stderr=(stderr+b.toString()).slice(-4096)});
 const ready=new Promise((resolve,reject)=>{
  const lines=createInterface({input:child.stdout});
  const timer=setTimeout(()=>finish(Error('Backend startup timed out')),timeout);
  const fail=e=>finish(e);const exit=code=>finish(Error('Backend exited before ready ('+code+'): '+stderr));
  function finish(error,url){clearTimeout(timer);lines.close();child.off('error',fail);child.off('exit',exit);error?reject(error):resolve(url)}
  child.once('error',fail);child.once('exit',exit);
  lines.on('line',line=>{const m=/^MONITOR_READY (http:\/\/127\.0\.0\.1:\d+)$/.exec(line);if(m)finish(null,m[1])});
 });
 // Keep an error handler even after startup, e.g. stdin closes unexpectedly.
 child.on('error',()=>{});child.stdin.on('error',()=>{});
 const stop=()=>new Promise(resolve=>{if(child.exitCode!==null||child.signalCode!==null){resolve();return};const timer=setTimeout(()=>{child.kill();resolve()},7000);child.once('exit',()=>{clearTimeout(timer);resolve()});child.stdin.end('shutdown\n')});
 return {child,ready,stop};
}
module.exports={startBackend};
