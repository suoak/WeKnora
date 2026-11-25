"""Read-only DB/index/file reconciliation for the final isolated candidate."""
from office_test_remote import connect,execute,REMOTE_ROOT
CODE='ROOT='+repr(REMOTE_ROOT)+'\n'+r'''
import datetime,hashlib,json,os,pathlib,socket,subprocess,time
root=pathlib.Path(ROOT);site=root/'site';project='knowhub-office-guard-test'
assert socket.gethostname()=='aqyfzx10' and os.geteuid()==1000
proof=json.loads((site/'deployment-proof.json').read_text());assert proof['source_sha']=='668d067ec75a2a351ace4f7f0797be77af2b24ae'
kb=json.loads((site/'acceptance-kb-668d067ec.json').read_text())['id']
assert __import__('re').fullmatch('[a-f0-9-]{36}',kb)
command=['docker','compose','-p',project,'-f',str(site/'compose.json')]
def sql(q):
 p=subprocess.run(command+['exec','-T','postgres','psql','-X','-U','postgres','-d','office_guard_test','-At','-v','ON_ERROR_STOP=1'],input=q,text=True,capture_output=True,timeout=30)
 assert p.returncode==0,p.stderr[-500:]
 return p.stdout.strip()
deadline=time.monotonic()+90
while time.monotonic()<deadline:
 pending=sql("SELECT count(*) FROM knowledges WHERE knowledge_base_id='"+kb+"' AND file_name IN ('browser-normal.docx','browser-protected.xlsx') AND parse_status IN ('pending','processing','finalizing');")
 if pending=='0':break
 time.sleep(2)
query="""SELECT COALESCE(json_agg(x),'[]') FROM (
 SELECT k.id,k.file_name,k.parse_status,k.enable_status,k.error_message,
 (SELECT count(*) FROM chunks c WHERE c.knowledge_id=k.id) AS chunk_count,
 (SELECT count(*) FROM embeddings e WHERE e.knowledge_id=k.id) AS index_count,
 k.file_path FROM knowledges k WHERE k.knowledge_base_id='"""+kb+"' ORDER BY k.file_name) x;"
rows=json.loads(sql(query))
encrypted_names=['example_password.docx','example_password.xlsx','drm.xlsx','user-drm.xlsx','drm-disguised.txt','disguised.txt']
absent=all(not any(x['file_name']==n for x in rows) for n in encrypted_names)
normal=[x for x in rows if x['file_name'] in ('normal.docx','normal.xlsx','normal.pptx','sheet-protected.xlsx','modify-protected.docx','plain.doc','plain.xls','plain.ppt','WPS_Spout_test.xlsx')]
browser=[x for x in rows if x['file_name'] in ('browser-normal.docx','browser-protected.xlsx')]
core=[x for x in normal if x['file_name'] in ('normal.docx','normal.xlsx','normal.pptx')]
result={'source_sha':proof['source_sha'],'executed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'kb_id':kb,
 'records':rows,'normal_document_count':len(normal),'normal_parse_index_pass':len(normal)==9 and all(x['parse_status']=='completed' and x['chunk_count']>0 and x['index_count']>0 for x in normal),
 'browser_document_count':len(browser),'browser_parse_index_pass':len(browser)==2 and all(x['parse_status']=='completed' and x['chunk_count']>0 and x['index_count']>0 for x in browser),
 'normal_core_parse_index_pass':len(core)==3 and all(x['parse_status']=='completed' and x['chunk_count']>0 and x['index_count']>0 for x in core),
 'legacy_doc_empty_control':'plain.doc has FIB ccpText=1 and fcMin=2048/fcMac=2049 (paragraph terminator only); zero chunks/indices are expected. See separate nonempty Apache POI control.',
 'rejected_documents_absent':absent,'sql_mutations':False}
path=site/'final-data-reconciliation.json';path.write_text(json.dumps(result,indent=2))
print(json.dumps(result))
'''
if __name__=='__main__':
 with connect() as client:raise SystemExit(execute(client,CODE,150))
