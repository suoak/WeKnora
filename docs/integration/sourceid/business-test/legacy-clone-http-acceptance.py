"""Execute HTTP business assertions inside isolated loopback; never export bodies."""
import http.cookiejar,json,pathlib,ssl,urllib.request,urllib.parse,urllib.error,traceback
ROOT=pathlib.Path('/fixture');BASE='http://127.0.0.1';ISSUER='https://127.0.0.1:18443'
origin='http://knowhub.ruijie.com.cn'
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*a,**k):return None
jar=http.cookiejar.CookieJar()
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(jar),urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=str(ROOT/'ca.pem'))),NoRedirect())
def req(path,method='GET',payload=None,token=None,mock=False,binary=False):
    url=(ISSUER if mock else BASE)+path
    headers={'Content-Type':'application/json'}
    if not mock:headers.update({'Host':'knowhub.ruijie.com.cn','Origin':origin,'X-KnowHub-OIDC-Result':'1','Sec-Fetch-Site':'same-origin'})
    if token:headers['Authorization']='Bearer '+token
    q=urllib.request.Request(url,method=method,headers=headers,data=json.dumps(payload).encode() if payload is not None else (b'' if method=='POST' else None))
    try:r=opener.open(q,timeout=15)
    except urllib.error.HTTPError as e:r=e
    with r:
        raw=r.read(16777216);location=r.headers.get('Location','')
        if binary:return r.status,len(raw),location
        try:data=json.loads(raw)
        except ValueError:data={}
        return r.status,data,location
def login(mode):
    assert req('/mode','POST',{'mode':mode},mock=True)[0]==200
    status,body,_=req('/api/v1/auth/oidc/url');assert status==200 and body['success']
    u=urllib.parse.urlsplit(body['authorization_url']);assert u.scheme+'://'+u.netloc==ISSUER
    status,_,callback=req(u.path+'?'+u.query,mock=True);assert status==302
    u=urllib.parse.urlsplit(callback);assert u.scheme+'://'+u.netloc==origin
    status,_,location=req(u.path+'?'+u.query);assert status==302
    if mode=='deny':assert 'oidc_error=provider_error' in location;return None
    assert 'oidc_complete=1' in location
    status,body,_=req('/api/v1/auth/oidc/result','POST');assert status==200 and body['success']
    assert body.get('token') and not body.get('is_new_user')
    # Current local-session implementation returns a bounded predecessor
    # token for SwitchTenant, but rejects its use at the Refresh endpoint.
    if body.get('refresh_token'):
        assert req('/api/v1/auth/refresh','POST',{'refreshToken':body['refresh_token']})[0]==401
    return body
def me(token,expected):
    status,body,_=req('/api/v1/auth/me',token=token);assert status==200 and body['success']
    assert body['data']['user']['id']==expected
    return body['data']
stage='start';out={'result':'FAIL','real_sid_login':False,'real_employee_binding':False}
try:
    baseline=json.loads((ROOT/'acceptance-baseline.json').read_text())
    stage='original_admin_first_oidc_login';admin=login('admin');token=admin['token']
    assert admin['user']['id']==baseline['admin_user_id'] and admin['user']['tenant_id']==baseline['admin_tenant_id']
    assert admin['user']['is_system_admin'] is True
    assert req('/portal',token=token,binary=True)[0]==200
    info=me(token,baseline['admin_user_id']);assert info['user']['is_system_admin'] is True
    out['admin_first_login']={'original_user_id_preserved':True,'home_tenant_preserved':True,'system_admin_preserved':True,'auth_me':200,'refresh_renewal_rejected':True,'portal_static_route':200}
    stage='admin_original_knowledge_access'
    kb=baseline['knowledge_base_id'];docs=baseline['document_ids']
    # Explicitly switch if the legacy KB belongs to another permitted space.
    if baseline['knowledge_tenant_id']!=admin.get('tenant',{}).get('id'):
        st,b,_=req('/api/v1/auth/switch-tenant','POST',{'tenant_id':baseline['knowledge_tenant_id'],'refresh_token':admin.get('refresh_token','')},token=token);assert st==200 and b['success'];token=b['token']
    assert req('/api/v1/knowledge-bases/'+kb,token=token)[0]==200
    assert req('/api/v1/knowledge-bases/'+kb+'/knowledge',token=token)[0]==200
    statuses=[]
    for doc in docs:
        assert req('/api/v1/knowledge/'+doc,token=token)[0]==200
        st,size,_=req('/api/v1/knowledge/'+doc+'/download',token=token,binary=True)
        statuses.append({'http_status':st,'nonempty':size>0});assert st==200 and size>0
    out['original_knowledge']={'kb_detail':200,'document_list':200,'downloads':statuses}
    stage='admin_logout_second_login'
    assert req('/api/v1/auth/logout','POST',token=token)[0]==200
    assert req('/api/v1/auth/me',token=token)[0]==401
    admin=login('admin');token=admin['token'];me(token,baseline['admin_user_id'])
    out['admin_second_login_same_account']=True
    stage='existing_forbidden_resource_fixture'
    st,b,_=req('/api/v1/tenants','POST',{'name':'isolated-denied-space','description':'synthetic-only'},token=token)
    assert st in (200,201)
    tenant=b.get('data',b);tenant_id=tenant['id']
    st,b,_=req('/api/v1/auth/switch-tenant','POST',{'tenant_id':tenant_id,'refresh_token':admin.get('refresh_token','')},token=token);assert st==200 and b['success'];deny_admin_token=b['token']
    st,b,_=req('/api/v1/knowledge-bases','POST',{'name':'isolated-denied-kb','type':'document','embedding_model_id':baseline['embedding_model_id']},token=deny_admin_token)
    assert st in (200,201);denied_kb=b.get('data',b)['id']
    assert req('/api/v1/knowledge-bases/'+denied_kb,token=deny_admin_token)[0]==200
    out['forbidden_fixture']={'exists_verified_as_admin':True,'separate_space':tenant_id!=baseline['knowledge_tenant_id']}
    assert req('/api/v1/auth/logout','POST',token=deny_admin_token)[0]==200
    stage='synthetic_viewer_login'
    viewer=login('viewer');vtoken=viewer['token'];me(vtoken,baseline['viewer_user_id'])
    assert viewer['user']['id']==baseline['viewer_user_id'] and viewer['user']['is_system_admin'] is False
    assert req('/api/v1/knowledge-bases/'+kb,token=vtoken)[0]==200
    denied=req('/api/v1/knowledge-bases/'+denied_kb,token=vtoken)[0];assert denied in (403,404)
    assert req('/api/v1/auth/switch-tenant','POST',{'tenant_id':tenant_id},token=vtoken)[0] in (403,404)
    out['viewer_acl']={'auth_me':200,'allowed_existing_kb':200,'existing_cross_space_denied':denied,'cross_space_switch_denied':True}
    assert req('/api/v1/auth/logout','POST',token=vtoken)[0]==200
    assert req('/api/v1/auth/me',token=vtoken)[0]==401
    stage='admission_deny'
    login('deny');out['mock_admission_deny']=True
    status,b,_=req('/status',mock=True);out['mock_request_counts']=b['counts'];assert b['counts']['token']==3 and b['counts']['userinfo']==3
    out['result']='PASS'
except Exception as e:
    out.update(failed_stage=stage,failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename==__file__])
print(json.dumps(out))
