"""Open-loop synthetic HTTP login arrivals. All credentials remain in memory."""
import concurrent.futures, collections, http.cookiejar, json, pathlib, ssl, threading, time, urllib.error, urllib.parse, urllib.request
R=pathlib.Path('/fixture'); context=ssl.create_default_context(cafile=str(R/'ca.pem'))
class NR(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a,**k):return None
def client():
 jar=http.cookiejar.CookieJar()
 return urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(jar),urllib.request.HTTPSHandler(context=context),NR()),jar
def req(o,path,method='GET',body=None,token=None,extra=None):
 headers={'Host':'knowhub.ruijie.com.cn','Content-Type':'application/json'}
 if token:headers['Authorization']='Bearer '+token
 if extra:headers.update(extra)
 url=path if path.startswith('https://127.0.0.1:8443/') else 'http://127.0.0.1'+path
 try:r=o.open(urllib.request.Request(url,headers=headers,method=method,data=json.dumps(body).encode() if body is not None else None),timeout=30)
 except urllib.error.HTTPError as e:r=e
 with r:
  raw=r.read();b=json.loads(raw) if raw[:1] in (b'{',b'[') else {}
  return r.status,b,r.headers.get('Location','')
def percentile(values,p):return round(sorted(values)[min(len(values)-1,int((len(values)-1)*p))],2) if values else None
def run_case(label,n,kind='existing',fault='normal',reuse=None):
 start=time.monotonic();sessions=[];mu=threading.Lock();rows=[]
 def login(i,due):
  time.sleep(max(0,due-time.monotonic()));beg=time.monotonic();stage='authorize';o,jar=client()
  prefix='existing' if kind=='existing' or kind=='mixed' and i%2==0 else 'new'
  who=(reuse[i] if reuse else 'capacity-'+label+'-'+prefix+'-'+str(i))
  expected_fault=fault if fault in ('limited','failure') and i%10==0 else ('delay' if fault=='delay' else 'normal')
  try:
   st,b,_=req(o,'/api/v1/auth/oidc/url');assert st==200
   auth=b['authorization_url'];assert urllib.parse.urlsplit(auth).hostname=='127.0.0.1'
   auth+='&'+urllib.parse.urlencode({'synthetic_user':who,'synthetic_case':expected_fault})
   st,_,loc=req(o,auth);assert st==302
   u=urllib.parse.urlsplit(loc);assert u.hostname=='knowhub.ruijie.com.cn'
   stage='callback';st,_,loc=req(o,u.path+'?'+u.query);assert st==302
   if expected_fault in ('limited','failure'):
    assert '#oidc_complete=1' not in loc
    with mu:rows.append(('EXPECTED_REJECT',1000*(time.monotonic()-beg),1000*(beg-due),who))
    return
   assert '#oidc_complete=1' in loc
   stage='result';st,b,_=req(o,'/api/v1/auth/oidc/result','POST',{},extra={'X-KnowHub-OIDC-Result':'1','Origin':'http://knowhub.ruijie.com.cn'});assert st==200
   assert b.get('success') and b.get('token') and 'refresh_token' not in b and not any('refresh' in c.name.lower() for c in jar)
   token=b['token'];user=b['user'];assert not user['is_system_admin'] and not user['can_access_all_tenants'] and user['tenant_id']==0
   stage='auth_me';assert req(o,'/api/v1/auth/me',token=token)[0]==200
   with mu:
    sessions.append((who,token,user['id']))
    rows.append(('PASS',1000*(time.monotonic()-beg),1000*(beg-due),who))
  except Exception:
   with mu:rows.append(('FAIL_'+stage,1000*(time.monotonic()-beg),1000*(beg-due),who))
 with concurrent.futures.ThreadPoolExecutor(max_workers=64) as pool:
  jobs=[]
  for i in range(n):
   due=start+i*60/n;time.sleep(max(0,due-time.monotonic()));jobs.append(pool.submit(login,i,due))
  for j in jobs:j.result()
 durations=[r[1] for r in rows];lags=[r[2] for r in rows];counts=collections.Counter(r[0] for r in rows)
 summary={'scenario':label,'population':n,'window_seconds':60,'planned_starts_per_second':round(n/60,3),'wall_seconds':round(time.monotonic()-start,3),'outcomes':dict(counts),'p50_ms':percentile(durations,.5),'p95_ms':percentile(durations,.95),'p99_ms':percentile(durations,.99),'max_start_lag_ms':round(max(lags),2),'fault':fault}
 assert len(rows)==n
 expected_reject=n//10 if fault in ('limited','failure') else 0
 summary['functional_pass']=counts['PASS']==n-expected_reject and counts['EXPECTED_REJECT']==expected_reject
 return summary,sessions
def main():
 results=[];all_sessions=[];identity_ids={}
 cases=[('e300',300,'existing','normal'),('e500',500,'existing','normal'),('j300',300,'new','normal'),('j500',500,'new','normal'),('m300',300,'mixed','normal'),('m500',500,'mixed','normal'),('delay500',500,'mixed','delay'),('limited500',500,'mixed','limited'),('failure500',500,'mixed','failure')]
 for label,n,kind,fault in cases:
  result,sessions=run_case(label,n,kind,fault);results.append(result);all_sessions.extend(sessions)
  if label=='m500':relogin=list(sessions)
  pathlib.Path('/tmp/capacity-progress.json').write_text(json.dumps({'scenarios':results}))
 # The outer controller expires these synthetic sessions in the disposable DB,
 # verifies old Access rejection, then authorizes a new transaction once each.
 pathlib.Path('/tmp/capacity-expiry-ready').write_text('ready')
 for _ in range(60):
  if pathlib.Path('/tmp/capacity-expiry-applied').exists():break
  time.sleep(1)
 else:raise RuntimeError('synthetic_expiry_coordination_timeout')
 o,_=client();assert all(req(o,'/api/v1/auth/me',token=t)[0]==401 for _,t,_ in relogin)
 ids=[w for w,_,_ in relogin];old_ids={w:u for w,_,u in relogin}
 result,sessions=run_case('expired500',500,'mixed','normal',reuse=ids);results.append(result)
 result['same_user_ids']=all(old_ids[w]==u for w,_,u in sessions);assert result['same_user_ids']
 # Explicitly log out every fresh surviving session; no plaintext artifacts.
 good=0
 for _,t,_ in sessions:
  if req(o,'/api/v1/auth/logout','POST',token=t)[0]==200 and req(o,'/api/v1/auth/me',token=t)[0]==401:good+=1
 status=req(o,'https://127.0.0.1:8443/status')[1]
 assert status['authorize']==4400 and status['token']==4400 and status['profile']==4300 and status['expired_code']==0 and status['rate_limited']==50 and status['failed']==50
 out={'result':'PASS' if all(x['functional_pass'] for x in results) and good==500 and status['reused_code']==0 else 'FAIL','scenarios':results,'mock_counts':status,'expired_old_access_rejected':True,'relogin_logout_revocations':good,'real_sid_called':False,'raw_identity_artifacts':False,'production_capacity_pass':False}
 print(json.dumps(out))
if __name__=='__main__':main()
