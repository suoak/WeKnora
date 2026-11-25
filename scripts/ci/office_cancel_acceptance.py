"""Abort real multipart requests through preview and directly at the isolated APP."""
from office_test_remote import connect,execute,REMOTE_ROOT
CODE='ROOT='+repr(REMOTE_ROOT)+'\n'+r'''
import datetime,json,pathlib,runpy,socket,subprocess,time,uuid
n=runpy.run_path(ROOT+'/office_site_acceptance.py');assert n['SHA']=='668d067ec75a2a351ace4f7f0797be77af2b24ae'
site=pathlib.Path(ROOT)/'site';kb=json.loads((site/'acceptance-kb-668d067ec.json').read_text())['id'];account=n['register']('owner')
cid=subprocess.check_output(n['COMPOSE']+['ps','-q','app'],text=True).strip()
app=json.loads(subprocess.check_output(['docker','inspect',cid],text=True))[0]
assert app['Config']['Labels']['com.docker.compose.project']=='knowhub-office-guard-test'
ip=app['NetworkSettings']['Networks']['knowhub-office-guard-test-private']['IPAddress']
assert ip.startswith('10.254.222.')
results=[]
for label,host,port in [('preview','127.0.0.1',39781),('direct_app',ip,8080)]:
 name='client-cancel-'+uuid.uuid4().hex+'.docx';boundary='OfficeCancel'+uuid.uuid4().hex
 file=(pathlib.Path(ROOT)/'samples/normal.docx').read_bytes()
 body=('--'+boundary+'\r\nContent-Disposition: form-data; name="file"; filename="'+name+'"\r\nContent-Type: application/octet-stream\r\n\r\n').encode()+file+('\r\n--'+boundary+'--\r\n').encode()
 headers=('POST /api/v1/knowledge-bases/'+kb+'/knowledge/file HTTP/1.1\r\nHost: '+host+':'+str(port)+'\r\nAuthorization: Bearer '+account['token']+'\r\nContent-Type: multipart/form-data; boundary='+boundary+'\r\nContent-Length: '+str(len(body))+'\r\nConnection: close\r\n\r\n').encode()
 before=n['snapshot']()['files']
 with socket.create_connection((host,port),timeout=5) as client:
  client.sendall(headers+body[:4096]);time.sleep(.25)
 # Cancellation is intentional: no HTTP response is claimed after disconnect.
 time.sleep(1)
 count=int(n['sql']("SELECT count(*) FROM knowledges WHERE knowledge_base_id='"+kb+"' AND file_name='"+name+"';"))
 after=n['snapshot']()['files']
 results.append({'case':'client_cancel_'+label,'result':'PASS' if count==0 and before==after else 'FAIL','knowledge_count':count,'files_unchanged':before==after,'http_status':'not observed; client intentionally disconnected','scope':'incomplete multipart cancellation, not a local-disk ReaderAt stall or server timeout'})
result={'source_sha':n['SHA'],'executed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'results':results}
(site/'client-cancel-results.json').write_text(json.dumps(result,indent=2));print(json.dumps(result))
'''
if __name__=='__main__':
 with connect() as client:raise SystemExit(execute(client,CODE,90))
