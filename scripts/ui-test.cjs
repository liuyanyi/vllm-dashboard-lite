const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
(async()=>{
 const b=await chromium.launch({headless:true,args:['--no-sandbox']});
 try {
  const p=await b.newPage({viewport:{width:1920,height:1080},deviceScaleFactor:1});
  const errors=[];p.on('pageerror',e=>errors.push(e.message));
  await p.goto('http://127.0.0.1:9090');
  await p.getByRole('heading',{name:'推理服务总览'}).waitFor();
  await p.getByRole('button',{name:'查看 mock-generation'}).click();
  await p.getByRole('button',{name:'新会话',exact:true}).waitFor();
  const before=(await (await p.request.get('http://127.0.0.1:9090/api/endpoints')).json())[0];
  await p.getByRole('button',{name:'新会话',exact:true}).click();
  await p.waitForFunction(async old=>(await (await fetch('/api/endpoints')).json())[0].run!==old,before.run);
  await p.waitForFunction(old=>Array.from(document.querySelector('select[aria-label="观测会话"]').options).some(o=>o.value===String(old)),before.run);
  await p.getByRole('combobox',{name:'观测会话'}).selectOption(String(before.run));
  await p.getByRole('combobox',{name:'观测会话'}).selectOption('');
  await p.getByRole('button',{name:'总览',exact:true}).click();
  await p.screenshot({path:process.env.SCREENSHOT_PATH || '/tmp/vllm-monitor-1080p.png'});
  if(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('horizontal overflow');
  if(await p.getByRole('button',{name:'添加服务',exact:true}).count())throw Error('unexpected dynamic configuration UI');
  if(errors.length)throw Error(errors.join('\n'));
  console.log('PASS: overview, detail, manual session, history, YAML-only controls, 1080p layout, no JS errors');
 }finally{await b.close()}
})().catch(e=>{console.error(e);process.exit(1)});
