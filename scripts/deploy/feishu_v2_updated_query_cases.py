"""Generate private new-version search cases by comparing restored old indexes."""
import sys
import feishu_v2_control_audit as audit

REMOTE = audit.REMOTE[:audit.REMOTE.index("clock=redis('TIME')")] + r'''
if project!='knowhub-feishu-v2-d6e99d894d3a':raise ValueError('isolated_project_required')
baseline='feishu_accelerated_restore_20261027'
proof=json.loads((site/'accelerated-two-source-20261027'/'backup.json').read_text())
if proof.get('restored')!=baseline or not proof.get('verified_rows'):raise ValueError('verified_old_baseline_required')
def query(statement,database='knowhub_feishu_v2_test'):
 return json.loads(run(compose+['exec','-T','postgres','psql','-qXAt','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],"BEGIN READ ONLY; SET LOCAL statement_timeout='10s'; "+statement+"; COMMIT;").strip())
rows=query("SELECT coalesce(json_agg(t),'[]') FROM (SELECT m.knowledge_id,m.resource_type FROM feishu_personal_imports m JOIN users u ON u.id=m.user_id JOIN knowledge_bases kb ON kb.id=m.kb_id WHERE u.username='test' AND kb.name='test001' AND m.resource_type IN ('sheet','bitable')) t")
path=site/'real-table-query-cases.json'
if path.is_symlink():raise ValueError('unsafe_query_case_path')
cases=json.loads(path.read_text())
backup=site/'real-table-query-cases.before-update.json'
if not backup.exists():backup.write_text(json.dumps(cases));backup.chmod(0o600)
receipts=[]
for row in rows:
 kid=row['knowledge_id']
 if not re.fullmatch('[a-zA-Z0-9-]{1,64}',kid):raise ValueError('invalid_knowledge_identifier')
 statement="SELECT coalesce(json_agg(content),'[]') FROM chunks WHERE knowledge_id='"+kid+"' AND deleted_at IS NULL"
 old=query(statement,baseline);new=query(statement)
 old_text='\n'.join(old);old_lines=set(old_text.splitlines())
 lines=[line.strip() for text in new for line in text.splitlines() if line.strip() and line not in old_lines]
 added=[]
 for line in lines:
  if len(line)>800 or 'http' in line or 'base64' in line:continue
  for token in re.findall(r'[A-Za-z0-9_\u4e00-\u9fff]{3,40}',line):
   if token in old_text or token in added or token.isdigit():continue
   if len(token)<3:continue
   case={'kid':kid,'query':token,'expected':[token],'qualification':'new_value_absent_from_verified_old_index'}
   if case not in cases:cases.append(case)
   added.append(token)
   break
  if len(added)>=2:break
 receipts.append({'resource_type':row['resource_type'],'knowledge_hash':mask(kid),'new_value_case_count':len(added),'new_value_digests':[mask(v) for v in added],'old_chunk_count':len(old),'new_chunk_count':len(new)})
path.write_text(json.dumps(cases));path.chmod(0o600)
print(json.dumps({'status':'PREPARED','cases':receipts,'source_or_index_writes':False,'document_text_printed':False}))
'''

if __name__ == '__main__':
    audit.transport.REMOTE=REMOTE
    raise SystemExit(audit.transport.main())
