const {app,BrowserWindow,dialog,Menu}=require('electron');
const path=require('node:path');const fs=require('node:fs');
const {startBackend}=require('./backend.cjs');
let backend,window,quitting=false,allowQuit=false;
if(!app.requestSingleInstanceLock()){app.quit()}else{
 app.on('second-instance',()=>{if(window){if(window.isMinimized())window.restore();window.show();window.focus()}});
 app.whenReady().then(async()=>{
  const binary=path.join(app.isPackaged?path.join(process.resourcesPath,'backend'):path.join(__dirname,'bin'),process.platform==='win32'?'vllm-monitor.exe':'vllm-monitor');
  const db=process.env.VLLM_DASHBOARD_DB?path.resolve(process.env.VLLM_DASHBOARD_DB):path.join(app.getPath('userData'),'monitor.db');
  fs.mkdirSync(path.dirname(db),{recursive:true});
  backend=startBackend(binary,db);
  backend.child.on('exit',()=>{if(!quitting){dialog.showErrorBox('服务已停止','本地采集器已退出，请重新启动应用。');app.quit()}});
  const origin=await backend.ready;
  window=new BrowserWindow({width:1440,height:960,minWidth:1000,minHeight:700,title:'vLLM Dashboard Lite',show:false,backgroundColor:'#fafbfc',webPreferences:{nodeIntegration:false,contextIsolation:true,sandbox:true}});
  Menu.setApplicationMenu(null);
  window.webContents.session.setPermissionRequestHandler((_wc,_p,cb)=>cb(false));
  window.webContents.setWindowOpenHandler(()=>({action:'deny'}));
  window.webContents.on('will-navigate',(event,url)=>{if(new URL(url).origin!==origin)event.preventDefault()});
  window.webContents.session.webRequest.onHeadersReceived((details,callback)=>callback({responseHeaders:{...details.responseHeaders,'Content-Security-Policy':["default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-src 'none'"]}}));
  window.once('ready-to-show',()=>window.show());
  await window.loadURL(origin);
 }).catch(async error=>{dialog.showErrorBox('启动失败',String(error));app.quit()});
 app.on('window-all-closed',()=>app.quit());
 app.on('before-quit',event=>{if(allowQuit)return;event.preventDefault();if(quitting)return;quitting=true;Promise.resolve(backend?.stop()).finally(()=>{allowQuit=true;app.quit()})});
}
