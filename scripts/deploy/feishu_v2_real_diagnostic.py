"""Upgrade only the existing isolated test project, preserving users and OAuth.

Requires five passing exact-source gates, verified images, a stopped-worker
database snapshot and a successful restore. Original public entry is untouched.
Credentials are read through stdin; reports never include environment values.
"""
import argparse
import base64
import hashlib
import json
import re
import sys
import textwrap
from pathlib import Path, PurePosixPath

import paramiko

REMOTE = r"""
import json,pathlib,subprocess,hashlib,re,datetime,collections
root=pathlib.Path(ROOT);site=root/'isolated-site'
proof=json.loads((site/'preparation.json').read_text());project='knowhub-feishu-v2-'+OLD_SHA[:12]
if proof['project']!=project or root.is_symlink():raise ValueError('isolated_project_mismatch')
compose=['docker','compose','--project-name',project,'--file',str(site/'compose.json')]
def run(args,timeout=30,input=None):return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,input=input,timeout=timeout)
def query(q):
 p=run(compose+['exec','-T','postgres','psql','-qX','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_feishu_v2_test','-At'],input="BEGIN READ ONLY; SET LOCAL statement_timeout='10s'; "+q+"; COMMIT;")
 if p.returncode:raise ValueError('readonly_query_failed')
 return json.loads(p.stdout.strip() or 'null')
schema=query("SELECT json_object_agg(table_name,names) FROM (SELECT table_name,json_agg(column_name) names FROM information_schema.columns WHERE table_schema='public' GROUP BY table_name) x")
def cols(table):return schema.get(table,[])
def quoted(v):
 if not re.fullmatch('[a-zA-Z0-9_-]{1,64}',str(v)):raise ValueError('unexpected_identifier')
 return "'"+str(v)+"'"
def rows(table,fields,where):
 available=cols(table);fields=[f for f in fields if f in available]
 return query('SELECT coalesce(json_agg(q),\'[]\') FROM (SELECT '+','.join(fields)+' FROM '+table+' WHERE '+where+' LIMIT 100) q')
def mask(v):return hashlib.sha256(str(v).encode()).hexdigest()[:12]
def safe(rows):
 result=[]
 for row in rows:
  value={}
  for k,v in row.items():
   if k=='manifest_key':value[k]=bool(v)
   elif k=='id' or k.endswith('_id') or k in ['scope_id']:value[k]=mask(v) if v else None
   else:value[k]=v
  result.append(value)
 return result
users=rows('users',['id','is_active'],"username='test' AND deleted_at IS NULL")
if len(users)!=1:raise ValueError('expected_test_user_missing')
uid=users[0]['id']
batches=rows('feishu_import_batches',['id','kb_id','auth_mode','created_at','publication_consent_at'],"owner_id="+quoted(uid)+' ORDER BY created_at DESC')[:5]
bids=','.join(quoted(r['id']) for r in batches) or "''"
items=rows('feishu_import_batch_items',['id','batch_id','ordinal','status','error_category','import_id','retry_count','lease_until','updated_at','sharing_approval_id'], 'batch_id IN ('+bids+') ORDER BY updated_at DESC')
iids=','.join(quoted(r['import_id']) for r in items if r.get('import_id')) or "''"
imports=rows('feishu_personal_imports',['id','kb_id','knowledge_id','resource_type','status','updated_at','error_category'], 'id IN ('+iids+')')
canonical_count=query('SELECT to_json(count(DISTINCT (resource_type,document_token))) FROM feishu_personal_imports WHERE id IN ('+iids+')')
resource_states=rows('feishu_personal_resources',['import_id','logical_knowledge_id','active_version_id','candidate_version_id','current_attempt_id','generation'],'import_id IN ('+iids+')')
sources=rows('data_sources',['id','knowledge_base_id','type','status','legacy_access_state','sync_mode','sync_schedule','last_sync_at','updated_at'],'id IN ('+iids+')')
cursor_checks=query("SELECT coalesce(json_agg(q),'[]') FROM (SELECT id,last_sync_cursor IS NOT NULL AND last_sync_cursor::text NOT IN ('null','{}','') AS has_cursor FROM data_sources WHERE id IN ("+iids+")) q")
kb_list=','.join(quoted(v['kb_id']) for v in imports) or "''"
wiki_configuration=query("SELECT coalesce(json_agg(q),'[]') FROM (SELECT id AS kb_id,coalesce((indexing_strategy->>'wiki_enabled')::boolean,false) AS wiki_enabled FROM knowledge_bases WHERE id IN ("+kb_list+") ) q")
targets=query("SELECT coalesce(json_agg(q),'[]') FROM (SELECT kb.id,kb.tenant_id,kb.creator_id,"+
 "coalesce(fp.owner_id='"+uid+"',false) AS proven_private_owner,"+
 "EXISTS(SELECT 1 FROM tenant_members tm WHERE tm.user_id='"+uid+"' AND tm.tenant_id=kb.tenant_id AND tm.status='active' AND tm.deleted_at IS NULL) AS active_member "+
 "FROM knowledge_bases kb LEFT JOIN feishu_private_kbs fp ON fp.kb_id=kb.id WHERE kb.name='test001' AND kb.deleted_at IS NULL LIMIT 10) q")
private_dependencies=query("SELECT coalesce(json_agg(q),'[]') FROM (SELECT fp.kb_id,"+
 "(SELECT count(*) FROM knowledges k WHERE k.knowledge_base_id=fp.kb_id AND k.deleted_at IS NULL) AS knowledge_count,"+
 "(SELECT count(*) FROM feishu_import_batches b WHERE b.kb_id=fp.kb_id) AS batch_count,"+
 "(SELECT count(*) FROM feishu_personal_imports m WHERE m.kb_id=fp.kb_id) AS import_count,"+
 "(SELECT count(*) FROM chunks c WHERE c.knowledge_base_id=fp.kb_id AND c.deleted_at IS NULL) AS chunk_count,"+
 "(SELECT count(*) FROM embeddings e WHERE e.knowledge_base_id=fp.kb_id) AS vector_count "+
 "FROM feishu_private_kbs fp WHERE fp.owner_id='"+uid+"' LIMIT 10) q")
target_ids=','.join(quoted(v['id']) for v in targets) or "''"
approval_summary=query("SELECT coalesce(json_agg(q),'[]') FROM (SELECT resource_type,state,"+
 "expires_at>clock_timestamp() AS within_validity,count(*) AS count FROM feishu_sharing_approvals "+
 "WHERE owner_id="+quoted(uid)+" AND kb_id IN ("+target_ids+") GROUP BY resource_type,state,within_validity) q")
attempts=rows('feishu_personal_attempts',['id','import_id','logical_knowledge_id','candidate_version_id','manifest_key','status','retry_after','retry_count','last_error_category','lease_until','created_at','updated_at','manifest_size'], 'import_id IN ('+iids+')')
kids=sorted({r.get('knowledge_id') for r in imports if r.get('knowledge_id')}|{r.get('logical_knowledge_id') for r in attempts if r.get('logical_knowledge_id')})
kids=sorted(set(kids)|{r.get('candidate_version_id') for r in attempts if r.get('candidate_version_id')})
klist=','.join(quoted(x) for x in kids) or "''"
processing_stages=rows('knowledge_processing_spans',['knowledge_id','attempt','kind','name','status','error_code','started_at','finished_at','duration_ms'],"knowledge_id IN ("+klist+") AND kind IN ('root','stage') ORDER BY attempt DESC,id DESC")
knowledge=rows('knowledges',['id','knowledge_base_id','parse_status','enable_status','summary_status','created_at','updated_at','process_started_at','process_completed_at'], 'id IN ('+klist+')')
ops=rows('task_pending_ops',['id','scope','scope_id','task_type','op','fail_count','enqueued_at','claimed_at'], 'scope_id IN ('+klist+')'+(' OR '+ ' OR '.join("payload::text LIKE '%"+x+"%'" for x in kids) if kids else ''))
chunks=query('SELECT coalesce(json_agg(q),\'[]\') FROM (SELECT knowledge_id,count(*) AS count FROM chunks WHERE knowledge_id IN ('+klist+') GROUP BY knowledge_id) q')
chunk_shapes=query("SELECT coalesce(json_agg(q),'[]') FROM (SELECT knowledge_id,chunk_type,index_status,is_enabled,count(*) AS count FROM chunks WHERE knowledge_id IN ("+klist+") AND deleted_at IS NULL GROUP BY knowledge_id,chunk_type,index_status,is_enabled) q")
sync_history=rows('sync_logs',['id','data_source_id','status','started_at','finished_at','items_total','items_created','items_updated','items_deleted','items_skipped','items_failed','execution_generation','execution_claimed','created_at','updated_at'],'data_source_id IN ('+iids+') ORDER BY created_at DESC')
vectors=query('SELECT coalesce(json_agg(q),\'[]\') FROM (SELECT knowledge_id,dimension,count(*) AS count FROM embeddings WHERE knowledge_id IN ('+klist+') GROUP BY knowledge_id,dimension) q')
bm25=[]
for kid in sorted({r.get('knowledge_id') for r in imports if r.get('knowledge_id')}):
 term=query("SELECT to_json(coalesce(substring(content from '[A-Za-z]{4,}'),substring(content from '[一-鿿]{2,8}'))) FROM embeddings WHERE knowledge_id="+quoted(kid)+" AND content IS NOT NULL LIMIT 1")
 if term and re.fullmatch(r'[A-Za-z]{4,}|[\u4e00-\u9fff]{2,8}',term):
  count=query("SELECT to_json(count(*)) FROM embeddings WHERE knowledge_id="+quoted(kid)+" AND content ||| '"+term+"'")
  bm25.append({'knowledge_id':kid,'matching_index_rows':count,'status':'PASS' if count else 'NO_MATCH'})
 else:bm25.append({'knowledge_id':kid,'status':'NO_SAFE_QUERY_TERM'})
table_audit=[];table_query_cases=[]
if XLSX_AUDIT:
 import io,zipfile,xml.etree.ElementTree as ET
 environment=dict(line.split('=',1) for line in (site/'app.env').read_text().splitlines() if '=' in line)
 staging=pathlib.PurePosixPath(environment['FEISHU_PERSONAL_STAGING_DIR'])
 if not staging.is_absolute():raise ValueError('invalid_staging_root')
 ns={'s':'http://schemas.openxmlformats.org/spreadsheetml/2006/main'}
 source_credential=None
 connection=query("SELECT row_to_json(c) FROM (SELECT tokens,expires_at FROM feishu_connections WHERE user_id="+quoted(uid)+") c")
 if connection and datetime.datetime.fromisoformat(connection['expires_at'])>datetime.datetime.now(datetime.timezone.utc):
  import base64,urllib.request,urllib.error,urllib.parse
  from cryptography.hazmat.primitives.ciphers.aead import AESGCM
  cipher=connection['tokens']
  if not cipher.startswith('enc:v1:'):raise ValueError('encrypted_connection_required')
  encrypted=base64.urlsafe_b64decode(cipher[7:]+'='*((-len(cipher[7:]))%4))
  source_credential=json.loads(AESGCM(environment['SYSTEM_AES_KEY'].encode()).decrypt(encrypted[:12],encrypted[12:],None))
  if source_credential.get('owner')!=uid:raise ValueError('connection_owner_mismatch')
  class NoAuditRedirect(urllib.request.HTTPRedirectHandler):
   def redirect_request(self,*args,**kwargs):return None
  source_opener=urllib.request.build_opener(NoAuditRedirect())
 def source_get(path,body=None):
  if not source_credential:raise ValueError('authorization_expired_or_missing')
  if not path.startswith('/open-apis/') or '#' in path:raise ValueError('unexpected_source_api')
  headers={'Authorization':'Bearer '+source_credential['access_token']}
  if body is not None:headers['Content-Type']='application/json'
  request=urllib.request.Request('https://open.feishu.cn'+path,headers=headers,data=json.dumps(body).encode() if body is not None else None)
  try:
   with source_opener.open(request,timeout=20) as response:
    content=response.read(4*1024*1024+1)
    if len(content)>4*1024*1024:raise ValueError('source_response_limit')
    result=json.loads(content)
    if result.get('code')!=0:raise ValueError('feishu_api_code_'+str(int(result.get('code',-1))))
    return result['data']
  except urllib.error.HTTPError as failure:raise ValueError('feishu_http_'+str(failure.code))
  except urllib.error.URLError:raise ValueError('source_network_failure')
 def source_list(path):
  result=[];page='';seen=set()
  for iteration in range(20):
   data=source_get(path+'?page_size=100'+('&page_token='+urllib.parse.quote(page,safe='') if page else ''))
   result.extend(data['items'])
   if not data.get('has_more'):
    if data.get('total') is not None and len(result)!=data['total']:raise ValueError('source_count_changed')
    return result
   page=data.get('page_token')
   if not page or page in seen:raise ValueError('source_pagination_invalid')
   seen.add(page)
  raise ValueError('source_page_limit')
 for imported in imports:
  if imported['resource_type'] not in ['sheet','bitable']:continue
  related=[a for a in attempts if a['import_id']==imported['id']]
  if len(related)!=1:raise ValueError('ambiguous_table_attempt')
  attempt=related[0]
  key=attempt['manifest_key'] or ''
  if not re.fullmatch(r'[a-f0-9-]{36}/[a-f0-9-]{36}/[A-Za-z0-9_-]+',key):raise ValueError('unsafe_manifest_key')
  directory=staging/key
  def read_candidate(name):
   response=subprocess.run(compose+['exec','-T','app','cat',str(directory/name)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
   if response.returncode or len(response.stdout)>50*1024*1024:raise ValueError('candidate_read_failed')
   return response.stdout
  manifest=json.loads(read_candidate('manifest.json'));data=read_candidate('content')
  if manifest['attempt_id']!=attempt['id'] or manifest['candidate_id']!=attempt['candidate_version_id'] or manifest['import_id']!=imported['id']:raise ValueError('candidate_owner_mismatch')
  if manifest.get('assets') or len(data)!=manifest['size'] or hashlib.sha256(data).hexdigest()!=manifest['sha256']:raise ValueError('candidate_integrity_mismatch')
  parsed=rows('chunks',['content','metadata'],'knowledge_id='+quoted(imported['knowledge_id'])+' AND deleted_at IS NULL ORDER BY chunk_index')
  joined='\n'.join(str(c.get('content') or '') for c in parsed)
  sheets=[]
  with zipfile.ZipFile(io.BytesIO(data)) as archive:
   if sum(i.file_size for i in archive.infolist())>100*1024*1024:raise ValueError('workbook_expanded_limit')
   date_styles=set()
   if 'xl/styles.xml' in archive.namelist():
    styles=ET.fromstring(archive.read('xl/styles.xml'))
    formats={int(f.get('numFmtId')):f.get('formatCode','') for f in styles.findall('s:numFmts/s:numFmt',ns)}
    for style_index,style in enumerate(styles.findall('s:cellXfs/s:xf',ns)):
     format_id=int(style.get('numFmtId','0'));format_code=formats.get(format_id,'')
     if 14<=format_id<=22 or (format_id>=164 and re.search(r'[yd]',re.sub(r'"[^"]*"','',format_code),re.I)):date_styles.add(style_index)
   workbook=ET.fromstring(archive.read('xl/workbook.xml'))
   sheet_names=[s.get('name') for s in workbook.findall('s:sheets/s:sheet',ns)]
   properties=workbook.find('s:workbookPr',ns)
   date_epoch=datetime.datetime(1904,1,1) if properties is not None and properties.get('date1904') in ['1','true'] else datetime.datetime(1899,12,30)
   strings=[]
   if 'xl/sharedStrings.xml' in archive.namelist():
    strings=[''.join(t.text or '' for t in el.findall('.//s:t',ns)) for el in ET.fromstring(archive.read('xl/sharedStrings.xml')).findall('s:si',ns)]
   names=sorted(n for n in archive.namelist() if re.fullmatch(r'xl/worksheets/sheet[0-9]+\.xml',n))
   for ordinal,name in enumerate(names):
    workbook_rows=[];positioned_rows=[];formula_count=0;date_cells=0
    for row in ET.fromstring(archive.read(name)).findall('.//s:sheetData/s:row',ns):
     cells=[];positioned={}
     for cell in row.findall('s:c',ns):
      value=cell.find('s:v',ns);text=value.text if value is not None else ''
      if cell.get('t')=='s':text=strings[int(text)] if text else ''
      if cell.get('t')=='inlineStr':text=''.join(t.text or '' for t in cell.findall('.//s:t',ns))
      if text and cell.get('t') not in ['s','inlineStr'] and int(cell.get('s','0')) in date_styles:
       value=date_epoch+datetime.timedelta(days=float(text));text=value.isoformat(sep=' ',timespec='seconds')
       if text.endswith(' 00:00:00'):text=text[:10]
       date_cells+=1
      if cell.find('s:f',ns) is not None:formula_count+=1
      if text and text.strip():
       cells.append(text.strip());positioned[re.sub('[0-9]+$','',cell.get('r',''))]=text.strip()
     if cells:
      workbook_rows.append(cells);positioned_rows.append((int(row.get('r')),positioned))
    headers=workbook_rows[0] if workbook_rows else []
    for row_ordinal,row_values in enumerate(workbook_rows[1:2],1):
     comparable=[v for v in row_values if len(v.strip())>=2 and not re.search(r'https?://',v)]
     if len(comparable)>=2:
      table_query_cases.append({'kid':imported['knowledge_id'],'kb':imported['kb_id'],'sheet_ordinal':ordinal,'row_ordinal':row_ordinal,'query':comparable[0][:60],'expected':comparable[1:3]})
    values=[v for row in workbook_rows[1:] for v in row if len(v)>=2]
    aligned_rows=0;field_pairs=0;matched_pairs=0
    column_headers=positioned_rows[0][1] if positioned_rows else {}
    for physical_row,row_cells in positioned_rows[1:]:
     candidates=[c.get('content','') for c in parsed if isinstance(c.get('metadata'),dict) and c['metadata'].get('parser.sheet')==sheet_names[ordinal] and str(c['metadata'].get('parser.row'))==str(physical_row)]
     if any(all(v in content for v in row_cells.values()) for content in candidates):aligned_rows+=1
     for column,value in row_cells.items():
      if column not in column_headers:continue
      field_pairs+=1
      if any(column_headers[column]+': '+value in content for content in candidates):matched_pairs+=1
    sheets.append({'sheet_ordinal':ordinal,'nonempty_rows':len(workbook_rows),'header_cells':len(headers),'nonempty_data_cells':sum(len(r) for r in workbook_rows[1:]),'formula_cells':formula_count,'date_cells_normalized_from_excel_serial':date_cells,'header_values_found_in_chunks':sum(v in joined for v in headers),'comparable_values':len(values),'values_found_in_chunks':sum(v in joined for v in values)})
    sheets[-1].update(data_rows_with_all_values_at_original_row=aligned_rows,field_value_pairs=field_pairs,field_value_pairs_at_original_row=matched_pairs)
  row_metadata=sum(bool((c.get('metadata') or {}).get('parser.row')) for c in parsed if isinstance(c.get('metadata'),dict))
  source_audit={}
  try:
   identity=query('SELECT row_to_json(x) FROM (SELECT document_token,source_modified FROM feishu_personal_imports WHERE id='+quoted(imported['id'])+') x')
   token=identity['document_token']
   if not re.fullmatch('[A-Za-z0-9]+',token):raise ValueError('invalid_source_identity')
   def metadata_revision():
    metadata=source_get('/open-apis/drive/v1/metas/batch_query',{'request_docs':[{'doc_token':token,'doc_type':imported['resource_type']}],'with_url':False})
    if metadata.get('failed_list') or len(metadata.get('metas',[]))!=1:raise ValueError('source_metadata_unconfirmed')
    value=metadata['metas'][0]
    if value['doc_token']!=token or value['doc_type']!=imported['resource_type']:raise ValueError('source_metadata_identity_changed')
    return value['latest_modify_time']
   before_revision=metadata_revision()
   if imported['resource_type']=='bitable':
    base='/open-apis/bitable/v1/apps/'+token+'/tables'
    tables=source_list(base);source_tables=[]
    for index,table in enumerate(tables):
     table_id=table['table_id']
     if not re.fullmatch('[A-Za-z0-9]+',table_id):raise ValueError('invalid_table_identity')
     fields=source_list(base+'/'+table_id+'/fields');records=source_list(base+'/'+table_id+'/records')
     relationships=[f for f in fields if f.get('type') in [18,21]]
     source_tables.append({'table_ordinal':index,'field_count':len(fields),'record_count':len(records),'relation_fields':len(relationships),'fields_with_stable_id':sum(bool(f.get('field_id')) for f in fields),'records_with_stable_id':sum(bool(r.get('record_id')) for r in records),'relation_properties_in_chunks':sum(bool(f.get('property')) and json.dumps(f['property'],ensure_ascii=False) in joined for f in relationships)})
    source_audit={'status':'READ_SUCCESS','tables':source_tables,'xlsx_alone_proves_stable_source_identity_storage':False}
   else:
    response=source_get('/open-apis/sheets/v3/spreadsheets/'+token+'/sheets/query')
    source_audit={'status':'READ_SUCCESS','sheet_count':len(response['sheets']),'grid_cells':sum(s.get('grid_properties',{}).get('row_count',0)*s.get('grid_properties',{}).get('column_count',0) for s in response['sheets'])}
   after_revision=metadata_revision()
   source_audit['revision_unchanged_during_audit']=before_revision==after_revision
   source_audit['revision_matches_import']=str(before_revision)==str(identity['source_modified'])
  except ValueError as error:source_audit={'status':'UNCONFIRMED','error_category':str(error)}
  table_audit.append({'knowledge_id':mask(imported['knowledge_id']),'resource_type':imported['resource_type'],'candidate_integrity':'PASS','export_bytes':len(data),'sheets':sheets,'source_api':source_audit,'chunks_examined':len(parsed),'chunk_sample_limit':100,'chunks_with_row_metadata':row_metadata,'scope':'exported_workbook_to_chunks_and_readonly_source_inventory'})
 if source_credential:source_credential.clear()
 environment.clear()
 path=site/'real-table-query-cases.json'
 if path.is_symlink():raise ValueError('unexpected_query_case_path')
 fd=__import__('os').open(str(path),__import__('os').O_WRONLY|__import__('os').O_CREAT|__import__('os').O_TRUNC,0o600)
 with __import__('os').fdopen(fd,'w') as output:json.dump(table_query_cases,output)
 path.chmod(0o600)
if DB_ONLY:
 runtime_scheduler=[]
 for service in ['app','worker2']:
  cid=run(compose+['ps','--quiet',service]).stdout.strip()
  info=json.loads(run(['docker','inspect',cid]).stdout)[0]
  env=dict(line.split('=',1) for line in info['Config']['Env'] if '=' in line)
  scope=env.get('FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS','')
  scope_ids=[v for v in scope.split(',') if re.fullmatch('[a-f0-9-]{36}',v)]
  log=run(compose+['logs','--no-color','--since','20m','--tail','8000',service]).stdout
  registered=re.findall(r'\[Scheduler\] started with (\d+) cron entries',log)
  runtime_scheduler.append({'service':service,'running':info['State']['Running'],'health':info['State'].get('Health',{}).get('Status'),'docreader_address_configured':env.get('DOCREADER_ADDR')=='docreader:50051','allowed_source_hashes':sorted(mask(v) for v in scope_ids),'unrestricted_source_scope':scope in ['', 'all'],'latest_registered_cron_entries':int(registered[-1]) if registered else None,'sync_safety_enabled':env.get('FEISHU_SYNC_SAFETY_ENABLED')=='true'})
  runtime_scheduler[-1]['safety_events']=[{'event':event,'reason':reason} for event,reason in re.findall(r'\[FeishuSafety\] event=([a-z_0-9]+) reason=([a-z_0-9]+)',log)][-12:]
  runtime_scheduler[-1]['scheduled_source_gate_closed']=[{'source_hash':mask(source),'count':sum('sync safety gate closed' in line and source in line for line in log.splitlines())} for source in scope_ids]
  env.clear()
 link_matches=[]
 if APPROVED_LINKS:
  import urllib.parse
  def canonical_link(value):
   u=urllib.parse.urlsplit(value)
   return (u.scheme.lower(),u.netloc.lower(),u.path.rstrip('/'))
  allowed=[canonical_link(v) for v in APPROVED_LINKS]
  for row in rows('feishu_import_batch_items',['id','url'],'batch_id IN ('+bids+')'):
   value=canonical_link(row['url'])
   link_matches.append({'item_id':mask(row['id']),'approved_link_ordinal':allowed.index(value) if value in allowed else None})
 admission=[]
 if batches:
  timestamp=datetime.datetime.fromisoformat(batches[0]['created_at'])
  since=(timestamp-datetime.timedelta(seconds=30)).isoformat()
  until=(timestamp+datetime.timedelta(seconds=60)).isoformat()
  for service in ['app','worker2']:
   p=run(compose+['logs','--no-color','--timestamps','--since',since,'--until',until,'--tail','3000',service],30)
   lines=re.sub(r'\x1b\[[0-?]*[ -/]*[@-~]','',p.stdout+p.stderr).splitlines()
   patterns={'quick_import':r'POST.*?/api/v1/me/feishu/imports','advanced_batch':r'POST.*?/api/v1/knowledge-bases/[^/\s"]+/feishu/batches','continue_batch':r'POST.*?/api/v1/feishu/batches/[^/\s"]+/continue'}
   events=[]
   for name,pattern in patterns.items():
    for line in lines:
     if re.search(pattern,line):
      status=re.search(r'status_code[=:\s"]+([0-9]{3})',line)
      events.append({'route':name,'http_status':int(status.group(1)) if status else None})
   failure_lines=[line for line in lines if re.search(r'\b(ERROR|WARN)\b',line)]
   failure_markers=['Failed to convert','failed to convert','Failed to process','failed to process','unsupported','not supported','permission denied','SQLSTATE','does not exist','no such','empty','spreadsheet','data table','duckdb','DuckDB','xlsx','failed to load','cannot','refused','timeout','deadline','invalid','safety','budget','receipt','partial','requires review']
   failure_markers+=['not connected','not configured','not available','unavailable','docreader','DocReader','anydoc','excel','failed','schema','driver','connection','rpc','EOF','Unimplemented']
   diagnostics=[{'locations':re.findall(r'[A-Za-z0-9_]+\.go:[0-9]+',line),'codes':re.findall(r'\bcode=([a-zA-Z0-9_]+)',line),'markers':[v for v in failure_markers if v in line],'sqlstates':re.findall(r'SQLSTATE ([A-Z0-9]{5})',line),'fingerprint':mask(line)} for line in failure_lines]
   api_results=[{'operation':operation,'category':category,'code':int(code),'http_status':int(status)} for operation,category,code,status in re.findall(r'operation=([a-z_]+) category=([a-z_]+) code=([0-9]+) status=([0-9]+)','\n'.join(lines))]
   admission.append({'service':service,'route_counts':{name:sum(bool(re.search(pattern,line)) for line in lines) for name,pattern in patterns.items()},'events':events,'feishu_api_results':api_results,'failure_diagnostics':diagnostics,'scanned_lines':len(lines)})
 state={'user_id':uid,'batches':batches,'items':items,'imports':imports,'attempts':attempts,'knowledge':knowledge}
 path=site/'real-batch-readonly-state.json';path.write_text(json.dumps(state));path.chmod(0o600)
 print(json.dumps({'status':'READ_ONLY','runtime_scheduler':runtime_scheduler,'wiki_configuration':safe(wiki_configuration),'sync_history':safe(sync_history),'chunk_shapes':safe(chunk_shapes),'processing_stages':safe(processing_stages),'resource_states':safe(resource_states),'sources':safe(sources),'source_cursor_checks':safe(cursor_checks),'table_audit':table_audit,'approved_link_matches':link_matches,'admission_routes':admission,'approval_summary':approval_summary,'canonical_resource_count':canonical_count,
  'batches':safe(batches),'items':safe(items),'imports':safe(imports),'attempts':safe(attempts),
  'knowledge':safe(knowledge),'chunks':safe(chunks),'vectors':safe(vectors),'bm25':safe(bm25),'outbox':safe(ops)}))
 raise SystemExit(0)
logreports={}
for service in []:
 p=run(compose+['logs','--no-color','--since','30m','--tail','1500',service],45)
 lines=(p.stdout+p.stderr).splitlines()
 matching=[line for line in lines if any(x in line for x in [uid]+[r['id'] for r in attempts]+kids)]
 patterns={'reconcile_errors':r'(?i)reconcil.*(error|fail)|recover.*(error|fail)', 'metadata_api':r'operation=(resolve_personal_resource|get_wiki_node)', 'download_export':r'operation=.*(export|download)', 'processing':r'(?i)processdocument|manualprocess|postprocess','auth_error':r'(?i)forbidden|permission|unauthorized|reconnect','index_error':r'(?i)(embed|index).*(error|fail)','db_missing_column':r'(?i)column .* does not exist','not_found':r'(?i)record not found'}
 logreports[service]={'scanned_lines':len(lines),'related_lines':len(matching),'all_log_categories':{k:sum(bool(re.search(v,l)) for l in lines) for k,v in patterns.items()},'related_categories':{k:sum(bool(re.search(v,l)) for l in matching) for k,v in patterns.items()}}
 # Only stable failure identifiers are emitted, never raw logs or document URLs.
 codes=collections.Counter()
 for line in lines:
  for code in re.findall(r'(?:category=|error_category[=: ]+)([a-z_]{3,64})',line):codes[code]+=1
 logreports[service]['failure_categories']=dict(codes)
 known=['permission denied','context deadline exceeded','personal_resource_revision_changed','feishu_invalid_office_download','personal manifest ownership mismatch','empty personal candidate','connection refused','no space left','invalid personal manifest key']
 logreports[service]['known_errors']={v:sum(v in l.lower() for l in lines) for v in known}
 logreports[service]['api_events']=[{'operation':m.group(1),'http_status':int(m.group(2)),'bytes':int(m.group(3))} for l in lines for m in [re.search(r'operation=([a-z_]+) status=(\d+) bytes=(\d+)',l)] if m][-20:]
 health=run(compose+['exec','-T',service,'/bin/sh','-c','id -u; id -g; stat -c "%u:%g:%a" /data/feishu-staging; if test -w /data/feishu-staging; then echo writable; else echo not_writable; fi'])
 logreports[service]['staging_access']=health.stdout.strip().splitlines()

# Read only Redis protocol inspection; credentials remain in memory.
import socket
redisid=run(compose+['ps','--quiet','redis']).stdout.strip()
rrecord=json.loads(run(['docker','inspect',redisid]).stdout)[0]
ip=list(rrecord['NetworkSettings']['Networks'].values())[0]['IPAddress']
secret=re.search(r'^requirepass (.+)$',(site/'redis.conf').read_text(),re.M).group(1)
connection=socket.create_connection((ip,6379),timeout=10);stream=connection.makefile('rb')
def redis(*args):
 parts=[str(a).encode() for a in args];connection.sendall(b'*'+str(len(parts)).encode()+b'\r\n'+b''.join(b'$'+str(len(x)).encode()+b'\r\n'+x+b'\r\n' for x in parts))
 def read():
  line=stream.readline();kind=line[:1];value=line[1:-2]
  if kind==b'+':return value
  if kind==b'-':raise ValueError('redis_readonly_command_failed')
  if kind==b':':return int(value)
  if kind==b'$':
   size=int(value)
   if size<0:return None
   data=stream.read(size);stream.read(2);return data
  if kind==b'*':return [read() for _ in range(int(value))]
  raise ValueError('redis_response_invalid')
 return read()
redis('AUTH',secret)
queue=[];task_errors=[];cursor=0
for page in range(20):
 cursor,keys=redis('SCAN',cursor,'MATCH','*asynq*','COUNT',200)
 for raw in keys:
  key=raw.decode();kind=redis('TYPE',key)
  if kind in [b'list',b'zset']:
   queue.append({'queue_key_hash':mask(key),'kind':kind.decode(),'count':redis('LLEN' if kind==b'list' else 'ZCARD',key)})
  if kind==b'hash' and any(a['id'] in key for a in attempts):
   values=redis('HGETALL',key);values=dict(zip(values[::2],values[1::2]))
   err=values.get(b'error',values.get(b'err',values.get(b'last_err',b''))).decode(errors='replace')
   combined=b' '.join(values.values()).lower()
   proto=values.get(b'msg',b'');offset=0;wire_errors=[]
   def varint():
    global offset
    value=0;shift=0
    while True:
     b=proto[offset];offset+=1;value|=(b&127)<<shift
     if b<128:return value
     shift+=7
   while offset<len(proto):
    tag=varint();field=tag>>3;kind=tag&7
    if kind==0:varint()
    elif kind==2:
     size=varint();part=proto[offset:offset+size];offset+=size
     if field!=2 and b'permission denied' in part.lower():
      text=part.decode(errors='replace')
      wire_errors.append({'field':field,'permission_denied':True,'operation':next((op for op in ['mkdir','open','rename','write','read','stat'] if text.startswith(op+' ')),'other'),
       'staging_path': '/data/feishu-staging' in text, 'tmp_path':'/tmp/' in text,'path_component':next((x for x in ['manifest.json','content','assets'] if x in text),'other')})
    elif kind==1:offset+=8
    elif kind==5:offset+=4
    else:break

   known=['personal_resource_revision_changed_during_fetch','personal_resource_revision_changed','permission denied','context deadline exceeded','feishu_invalid_office_download','invalid personal manifest key','personal manifest ownership mismatch','personal recovery fence rejected','personal attempt fence rejected','personal_resource_revision_changed','personal manifest content mismatch','feishu_html_download_rejected','file exists','context canceled']
   category=next((x for x in known if x.encode() in combined),'unclassified' if err else '')
   task_errors.append({'task_id_hash':mask(key),'status':values.get(b'state',b'').decode(errors='replace'),'error_category':category,'error_fingerprint':mask(err) if err else None,'fields':[v.decode() for v in values.keys()],'wire_error_categories':wire_errors})
 if cursor==b'0':break
connection.close()
logreports['redis']={'queues':queue,'tasks':task_errors}
for service in ['app','worker2']:
 cid=run(compose+['ps','--quiet',service]).stdout.strip()
 process=run(compose+['exec','-T',service,'/bin/sh','-c','for p in /proc/[0-9]*/status; do grep -E "^(Name|Uid|Gid):" "$p" 2>/dev/null; done'])
 logreports[service]={'process_users':process.stdout.strip().splitlines(), 'staging_owner_mode':run(compose+['exec','-T',service,'stat','-c','%u:%g:%a','/data/feishu-staging']).stdout.strip()}


metadata=[]
if APPROVED_LINKS:
 import base64,urllib.request,urllib.error,urllib.parse
 from cryptography.hazmat.primitives.ciphers.aead import AESGCM
 links=[]
 for value in APPROVED_LINKS:
  u=urllib.parse.urlsplit(value)
  if u.scheme!='https' or u.netloc!='ruijie.feishu.cn' or not re.fullmatch('/wiki/[A-Za-z0-9]+',u.path) or u.query or u.fragment:raise ValueError('approved_wiki_url_required')
  links.append(u.path.rsplit('/',1)[1])
 if len(links)>10:raise ValueError('approved_link_limit')
 record=query("SELECT row_to_json(c) FROM (SELECT tokens,expires_at FROM feishu_connections WHERE user_id="+quoted(uid)+") c")
 if not record:metadata=[{'ordinal':i,'error_category':'authorization_missing'} for i in range(len(links))]
 elif datetime.datetime.fromisoformat(record['expires_at'])<=datetime.datetime.now(datetime.timezone.utc):
  metadata=[{'ordinal':i,'error_category':'authorization_expired','api_called':False} for i in range(len(links))]
 else:
  environment=dict(line.split('=',1) for line in (site/'app.env').read_text().splitlines() if '=' in line)
  cipher=record['tokens']
  if not cipher.startswith('enc:v1:'):raise ValueError('encrypted_connection_required')
  raw=base64.urlsafe_b64decode(cipher[7:]+'='*((-len(cipher[7:]))%4))
  credential=json.loads(AESGCM(environment['SYSTEM_AES_KEY'].encode()).decrypt(raw[:12],raw[12:],None))
  if credential.get('owner')!=uid:raise ValueError('connection_owner_mismatch')
  class NoRedirect(urllib.request.HTTPRedirectHandler):
   def redirect_request(self,*args,**kwargs):return None
  opener=urllib.request.build_opener(NoRedirect())
  for i,token in enumerate(links):
   request=urllib.request.Request('https://open.feishu.cn/open-apis/wiki/v2/spaces/get_node?token='+token,headers={'Authorization':'Bearer '+credential['access_token']})
   try:
    with opener.open(request,timeout=15) as response:
     result=json.loads(response.read(1024*1024));code=result.get('code')
     metadata.append({'ordinal':i,'http_status':response.status,'feishu_code':code,'resource_type':result.get('data',{}).get('node',{}).get('obj_type'),'status':'PASS' if code==0 else 'REJECTED'})
   except urllib.error.HTTPError as error:metadata.append({'ordinal':i,'http_status':error.code,'error_category':'feishu_api_rejected'})
   except urllib.error.URLError:metadata.append({'ordinal':i,'error_category':'network_failure'})
  credential.clear();environment.clear()
state={'user_id':uid,'batches':batches,'items':items,'imports':imports,'attempts':attempts,'knowledge':knowledge}
# Store identifiers/status only in the existing private site; do not alter jobs.
path=site/'real-batch-readonly-state.json';path.write_text(json.dumps(state));path.chmod(0o600)
print(json.dumps({'status':'READ_ONLY','canonical_resource_count':canonical_count,'wiki_configuration':safe(wiki_configuration),'private_kb_dependencies':safe(private_dependencies),'selected_target_check':safe(targets),'batches':safe(batches),'items':safe(items),'imports':safe(imports),'attempts':safe(attempts),'knowledge':safe(knowledge),'outbox':safe(ops),'chunks':safe(chunks),'vectors':safe(vectors),'bm25':safe(bm25),'approved_link_metadata':metadata,'logs':logreports}))
"""


def read_json(path):
    raw = path.read_bytes()
    return json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe', b'\xfe\xff')) else raw.decode('utf-8-sig'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--candidate-manifest', type=Path, required=True)
    parser.add_argument('--release-gates', type=Path)
    parser.add_argument('--test-username')
    parser.add_argument('--approved-links-file',type=Path)
    parser.add_argument('--db-only',action='store_true')
    parser.add_argument('--xlsx-audit',action='store_true')
    parser.add_argument('--mode', choices=['prepare', 'apply', 'finish', 'status', 'rollback'], required=True)
    args = parser.parse_args()
    old, new = read_json(args.manifest), read_json(args.candidate_manifest)
    for manifest in [old, new]:
        sha = manifest['source_sha']
        root = PurePosixPath(manifest['source_directory']).parent
        if (not re.fullmatch('[a-f0-9]{40}', sha) or root.parent != PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
                or not re.fullmatch(sha[:12] + r'-[a-f0-9]{8}', root.name)):
            raise SystemExit('invalid source manifest')
    client = paramiko.SSHClient()
    try:
        credential = json.loads(sys.stdin.read(16384))
        if credential.get('host') != '172.30.232.42' or credential.get('user') != 'aqyfzx':
            raise ValueError('unexpected_target')
        client.load_system_host_keys()
        known = Path.home() / '.ssh' / 'known_hosts'
        if known.is_file():
            client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'], username=credential['user'], password=credential['password'],
                       timeout=15, auth_timeout=15, banner_timeout=15, allow_agent=False, look_for_keys=False)
        credential.clear()
        values = {'ROOT': str(PurePosixPath(old['source_directory']).parent), 'CANDIDATE': str(PurePosixPath(new['source_directory']).parent),
                  'OLD_SHA': old['source_sha'], 'SHA': new['source_sha'], 'MODE': args.mode,
                  'GATES': read_json(args.release_gates) if args.release_gates else {}, 'TEST_USERNAME':args.test_username,
                  'APPROVED_LINKS':read_json(args.approved_links_file) if args.approved_links_file else [],
                  'DB_ONLY':args.db_only,'XLSX_AUDIT':args.xlsx_audit}
        guarded = 'try:\n' + textwrap.indent(REMOTE, ' ')
        guarded += "\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','reason':str(error) if isinstance(error,ValueError) else 'isolated_upgrade_operation_failed','error_type':type(error).__name__}))\n raise SystemExit(2)\n"
        code = base64.b64encode(('\n'.join(k + '=' + repr(v) for k, v in values.items()) + '\n' + guarded).encode()).decode()
        _, out, _ = client.exec_command("python3 -c \"import base64;exec(base64.b64decode('" + code + "'))\"", timeout=600)
        result = json.loads(out.read(65536))
        print(json.dumps(result, indent=2))
        return 2 if result.get('status') == 'BLOCKED' else 0
    except (OSError, ValueError, KeyError, TypeError, paramiko.SSHException) as error:
        print(json.dumps({'status': 'BLOCKED', 'reason': 'verified_isolated_upgrade_unavailable', 'error_type': type(error).__name__}))
        return 2
    finally:
        client.close()


if __name__ == '__main__':
    sys.exit(main())
