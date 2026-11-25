"""Download the three normal original Office files through authenticated API."""
from office_test_remote import connect,execute,REMOTE_ROOT
CODE='ROOT='+repr(REMOTE_ROOT)+'\n'+r'''
import datetime,hashlib,json,pathlib,runpy,urllib.request
n=runpy.run_path(ROOT+'/office_site_acceptance.py');assert n['SHA']=='668d067ec75a2a351ace4f7f0797be77af2b24ae'
root=pathlib.Path(ROOT);site=root/'site';kb=json.loads((site/'acceptance-kb-668d067ec.json').read_text())['id'];account=n['register']('owner')
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs):return None
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
rows=[]
for filename in ('normal.docx','normal.xlsx','normal.pptx'):
 kid=n['sql']("SELECT id FROM knowledges WHERE knowledge_base_id='"+kb+"' AND file_name='"+filename+"';")
 assert __import__('re').fullmatch('[a-f0-9-]{36}',kid)
 request=urllib.request.Request(n['BASE']+'/api/v1/knowledge/'+kid+'/download',headers={'Authorization':'Bearer '+account['token']})
 with opener.open(request,timeout=30) as response:
  data=response.read(2*1024*1024);status=response.status
 digest=hashlib.sha256(data).hexdigest();expected=hashlib.sha256((root/'samples'/filename).read_bytes()).hexdigest()
 rows.append({'filename':filename,'knowledge_id':kid,'http_status':status,'bytes':len(data),'sha256':digest,'source_sha256':expected,'matches_original':digest==expected})
result={'source_sha':n['SHA'],'executed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'result':'PASS' if all(x['matches_original'] and x['http_status']==200 for x in rows) else 'FAIL','originals':rows}
(site/'original-downloads.json').write_text(json.dumps(result,indent=2));print(json.dumps(result))
'''
if __name__=='__main__':
 with connect() as client:raise SystemExit(execute(client,CODE,130))
