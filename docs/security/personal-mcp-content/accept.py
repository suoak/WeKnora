"""Synthetic acceptance against only this task's loopback APP and PostgreSQL."""
import pathlib,json,urllib.request,urllib.error,base64,concurrent.futures,subprocess,time,zipfile,io,hashlib
B=pathlib.Path('/home/aqyfzx/ai/knowhub-personal-upload-r1-20261112');P=B.name
keys=json.loads((B/'private/clients.json').read_text());url='http://127.0.0.1:18873';results=[]
def sql(q):
 assert subprocess.check_output(['docker','inspect','--format','{{index .Config.Labels "knowhub.scope"}}',P+'-pg']).decode().strip()==P
 return subprocess.check_output(['docker','exec',P+'-pg','psql','-X','-U','postgres','-d','knowhub_personal_upload3046','-At','-v','ON_ERROR_STOP=1','-c',q],stderr=subprocess.DEVNULL).decode().strip()
def rpc(key,method,params=None,isolated=False):
 path='/internal/mcp/isolated/personal' if isolated else '/api/v1/mcp/personal'
 body=json.dumps({'jsonrpc':'2.0','id':1,'method':method,'params':params or {}}).encode()
 req=urllib.request.Request(url+path,data=body,headers={'Content-Type':'application/json','Accept':'application/json, text/event-stream','Authorization':'Bearer '+keys[key]})
 try:
  with urllib.request.urlopen(req,timeout=45) as r:data=json.loads(r.read())
 except urllib.error.HTTPError as e:return {'http_error':e.code}
 return data.get('result',data)
def call(name,args,key='key_a',isolated=False):return rpc(key,'tools/call',{'name':name,'arguments':args},isolated)
def decoded(r):return json.loads(r['content'][0]['text'])
def denied(r):return r.get('isError',False) or 'http_error' in r or 'error' in r
def check(name,passed,detail=None):
 results.append({'name':name,'pass':bool(passed),'detail':detail});(B/'accept-results.json').write_text(json.dumps(results,indent=2));assert passed,name
check('schema3046-before',sql('select version from schema_migrations')=='3046')
for isolated,n in [(False,12),(True,8)]:
 r=rpc('key_a','tools/list',isolated=isolated);tools=[t['name'] for t in r.get('tools',[])];check('isolated-tools' if isolated else 'external-tools',len(tools)==n,tools)
for name in ['add_document','upload_document','get_document_processing_status','delete_document']:
 check('isolated-denies-'+name,denied(call(name,{},isolated=True)))
rows=decoded(call('list_writable_knowledge_bases',{}));ids=[r['knowledge_base_id'] for r in rows]
check('writable-scope','upload-a-kb' in ids and 'upload-private-kb' in ids and 'upload-editor-kb' in ids and 'upload-b-kb' not in ids and 'upload-viewer-kb' not in ids,ids)
request={'knowledge_base_id':'upload-a-kb','title':'synthetic upload acceptance','content':'# KNOWHUB_UPLOAD_ACCEPTANCE_MARKER\nSynthetic document for isolated content acceptance.','idempotency_key':'accept-text-0001'}
for name,key,kb in [('same-space-denied','key_a','upload-b-kb'),('viewer-share-denied','key_a','upload-viewer-kb'),('retrieve-only-denied','key_read','upload-a-kb'),('fixed-scope-denied','key_fixed','upload-private-kb')]:
 r=call('add_document',dict(request,knowledge_base_id=kb,idempotency_key='denied-'+name),key);check(name,denied(r))
docs=[]
for label,kb in [('own','upload-a-kb'),('private','upload-private-kb'),('editor','upload-editor-kb')]:
 r=call('add_document',dict(request,knowledge_base_id=kb,idempotency_key='accept-text-'+label));check('text-'+label,not denied(r));docs.append(decoded(r)['document_id'])
fileargs={'knowledge_base_id':'upload-a-kb','filename':'synthetic.txt','content_base64':base64.b64encode(b'KNOWHUB_FILE_ACCEPTANCE_MARKER synthetic uploaded file content.').decode(),'idempotency_key':'accept-file-txt-0001'}
r=call('upload_document',fileargs);check('upload-txt',not denied(r));txtid=decoded(r)['document_id'];docs.append(txtid)
with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:replays=list(pool.map(lambda _:call('upload_document',fileargs),range(6)))
check('upload-concurrent-idempotent',all(not denied(r) and decoded(r)['document_id']==txtid for r in replays))
check('upload-payload-conflict',denied(call('upload_document',dict(fileargs,filename='different.txt'))))
for extension in ['md','pdf','docx','xlsx']:
 if extension=='md':data=b'# KNOWHUB_MARKDOWN_FILE_ACCEPTANCE_MARKER\nSynthetic markdown upload'
 elif extension=='pdf':
  stream=b'BT /F1 12 Tf 50 700 Td (KNOWHUB PDF FILE ACCEPTANCE MARKER) Tj ET';objects=[b'<</Type/Catalog/Pages 2 0 R>>',b'<</Type/Pages/Kids[3 0 R]/Count 1>>',b'<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Resources<</Font<</F1 4 0 R>>>>/Contents 5 0 R>>',b'<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>',b'<</Length '+str(len(stream)).encode()+b'>>\nstream\n'+stream+b'\nendstream'];data=b'%PDF-1.4\n';offsets=[0]
  for i,o in enumerate(objects):offsets.append(len(data));data+=str(i+1).encode()+b' 0 obj\n'+o+b'\nendobj\n'
  xref=len(data);data+=b'xref\n0 6\n0000000000 65535 f \n'+b''.join(('%010d 00000 n \n'%o).encode() for o in offsets[1:])+b'trailer<</Size 6/Root 1 0 R>>\nstartxref\n'+str(xref).encode()+b'\n%%EOF'
 else:
  b=io.BytesIO()
  main='word/document.xml' if extension=='docx' else 'xl/workbook.xml'
  namespace='http://schemas.openxmlformats.org/'
  files={'[Content_Types].xml':'<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/'+main+'" ContentType="application/vnd.openxmlformats-officedocument.'+('wordprocessingml.document.main+xml' if extension=='docx' else 'spreadsheetml.sheet.main+xml')+'"/></Types>','_rels/.rels':'<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="'+namespace+'officeDocument/2006/relationships/officeDocument" Target="'+main+'"/></Relationships>'}
  if extension=='docx':files[main]='<w:document xmlns:w="'+namespace+'wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>KNOWHUB DOCX FILE ACCEPTANCE MARKER</w:t></w:r></w:p></w:body></w:document>'
  else:
   files[main]='<workbook xmlns="'+namespace+'spreadsheetml/2006/main" xmlns:r="'+namespace+'officeDocument/2006/relationships"><sheets><sheet name="Synthetic" sheetId="1" r:id="rId1"/></sheets></workbook>'
   files['xl/_rels/workbook.xml.rels']='<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="'+namespace+'officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>'
   files['xl/worksheets/sheet1.xml']='<worksheet xmlns="'+namespace+'spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>KNOWHUB XLSX FILE ACCEPTANCE MARKER</t></is></c></row></sheetData></worksheet>'
  with zipfile.ZipFile(b,'w',zipfile.ZIP_DEFLATED) as z:
   for name,body in files.items():z.writestr(name,body)
  data=b.getvalue()
 r=call('upload_document',dict(fileargs,filename='synthetic.'+extension,content_base64=base64.b64encode(data).decode(),idempotency_key='accept-file-'+extension+'-0001'));check('upload-'+extension,not denied(r));docs.append(decoded(r)['document_id'])
# Processing results are preserved even when no parser/model is available.
statuses={}
for _ in range(30):
 for doc in docs:
  r=call('get_document_processing_status',{'document_id':doc});statuses[doc]=decoded(r) if not denied(r) else {'error':True}
 if all(s.get('status') in ('success','failed') for s in statuses.values()):break
 time.sleep(2)
(B/'processing-results.json').write_text(json.dumps(statuses,indent=2))
check('schema3046-after',sql('select version from schema_migrations')=='3046')
(B/'accept-done.json').write_text(json.dumps({'admission_pass':True,'all_processing_completed':all(s.get('status')=='success' for s in statuses.values()),'antivirus_scan':False}))
