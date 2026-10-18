import urllib.error
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a,**kw):return None
op=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect());out=[]
for port in (80,8080):
 try:r=op.open(urllib.request.Request('http://127.0.0.1:'+str(port)+'/api/v1/auth/oidc/callback',headers={'Host':'knowhub.ruijie.com.cn'}),timeout=10)
 except urllib.error.HTTPError as e:r=e
 with r:
  p=urllib.parse.urlsplit(r.headers.get('Location',''));q=urllib.parse.parse_qs(p.query)
  out.append({'port':port,'status':r.status,'path':p.path if p.path in ('/login','/auth/callback','/auth/oidc/callback') else 'OTHER','same_site_or_relative':p.hostname in (None,'knowhub.ruijie.com.cn'),'query_keys':list(q),'invalid_state_keys':[k for k,v in q.items() if v==['invalid_state']]})
print(json.dumps({'result':'READ_ONLY','callbacks':out,'real_sid_requests':0}))
