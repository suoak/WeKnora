import http from 'node:http'
import { readFileSync, existsSync } from 'node:fs'
import { resolve, extname, sep } from 'node:path'
const root=resolve(process.argv[2]),port=Number(process.argv[3]||4187)
const mime={'.html':'text/html; charset=utf-8','.js':'text/javascript; charset=utf-8','.css':'text/css; charset=utf-8','.json':'application/json','.svg':'image/svg+xml','.png':'image/png','.woff2':'font/woff2','.ico':'image/x-icon'}
http.createServer((req,res)=>{
  try {
    const path=new URL(req.url,'http://localhost').pathname
    if(/^\/(api|mcp|files)(\/|$)/.test(path)){res.writeHead(503,{'Content-Type':'application/json'});return res.end(JSON.stringify({success:false,message:'Isolation: API must be intercepted by the test fixture'}))}
    let file=resolve(root,'.'+decodeURIComponent(path))
    if(!file.startsWith(root+sep)){res.writeHead(403);return res.end()}
    if(!extname(file))file=resolve(root,'index.html')
    if(!existsSync(file)){res.writeHead(404);return res.end('Missing static resource')}
    res.writeHead(200,{'Content-Type':mime[extname(file)]||'application/octet-stream','Cache-Control':'no-store','Content-Security-Policy':"connect-src 'self'"})
    res.end(req.method==='HEAD'?undefined:readFileSync(file))
  }catch(e){res.writeHead(500);res.end(String(e))}
}).listen(port,'127.0.0.1',()=>console.log(`Frozen static build: http://127.0.0.1:${port}`))
