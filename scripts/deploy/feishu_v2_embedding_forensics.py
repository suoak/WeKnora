"""Read-only classified embedding failure evidence, never bodies or secrets."""
import sys
import argparse
import re
from pathlib import Path
import feishu_v2_control_audit as audit

EXTRA = r'''
failures=[]
for service in ['app','worker2']:
 logs=run(compose+['logs','--timestamps','--no-color','--since','2026-10-27T01:58:00Z','--tail','2000',service])
 for line in logs.splitlines():
  if not any(v in line.lower() for v in ['embedding','embed','status code: 400','http 400']):continue
  categories=[v for v in ['batch size','batch_size','input length','input must','max batch','less than','larger than','tokens','token limit','maximum context','invalid_parameter','invalid parameter','dimension','quota','sensitive','content filter','permission','http_400','status code: 400','status code:400'] if v in line.lower()]
  if not categories:continue
  at=re.search(r'\d{4}-\d{2}-\d{2}T[0-9:.]+Z',line)
  limits=re.findall(r'(?:batch size|less than|larger than|at most|maximum|max_tokens|dimension)[^0-9\n]{0,40}([0-9]{1,8})',line,re.I)
  codes=re.findall(r'\b(?:code[=: ]+|status code: ?)([a-zA-Z0-9_]{3,80})',line)
  failures.append({'service':service,'at':at[0] if at else None,'categories':categories,'numeric_limits':limits,'provider_codes':codes})
result['embedding_failure_categories']=failures[-30:]
source_failures=[]
for service in ['app','worker2']:
 logs=run(compose+['logs','--timestamps','--no-color','--since','2026-10-27T02:11:00Z','--tail','2000',service])
 for line in logs.splitlines():
  if 'failed to ingest item' not in line:continue
  reason=line.split('): ',1)[-1]
  source_failures.append({'service':service,'reason_digest':hashlib.sha256(reason.encode()).hexdigest()[:12],
   'source_defined_reasons':[v for v in KNOWN_REASONS if v.lower() in reason.lower()]})
result['source_failure_categories']=source_failures
query="SELECT coalesce(json_agg(t),'[]') FROM (SELECT ds.id AS source_id,r.state,r.source_revision,a.id,a.status,a.revision,a.source_hash,a.created_at,(a.manifest->'source_identity') IS NOT NULL AS has_package_identity,coalesce(a.manifest->'knowledge'->>'file_path','')<>'' AS has_archived_file FROM feishu_legacy_resources r JOIN data_sources ds ON ds.id=r.data_source_id JOIN feishu_legacy_attempts a ON a.resource_id=r.id JOIN feishu_personal_imports m ON m.id=ds.id JOIN users u ON u.id=m.user_id JOIN knowledge_bases kb ON kb.id=m.kb_id WHERE u.username='test' AND kb.name='test001' AND m.resource_type IN ('sheet','bitable') ORDER BY a.created_at DESC LIMIT 12) t;"
rows=json.loads(run(compose+['exec','-T','postgres','psql','-qXAt','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_feishu_v2_test'],query))
for row in rows:
 row['source_hash_id']=mask(row.pop('source_id'));row['attempt_hash']=mask(row.pop('id'));row['content_hash_prefix']=row.pop('source_hash')[:12]
result['source_attempt_observations']=rows
query="SELECT coalesce(json_agg(t),'[]') FROM (SELECT ds.id,m.resource_type,ds.status,ds.legacy_access_state,ds.sync_mode,ds.sync_schedule,strpos(coalesce(m.url,'')||ds.config::text,'BvNrwnQkuiRULpkRzA8c2nfTnoc')>0 AS matches_user_modified_link FROM data_sources ds JOIN feishu_personal_imports m ON m.id=ds.id JOIN users u ON u.id=m.user_id JOIN knowledge_bases kb ON kb.id=m.kb_id WHERE u.username='test' AND kb.name='test001' AND m.resource_type IN ('sheet','bitable')) t;"
rows=json.loads(run(compose+['exec','-T','postgres','psql','-qXAt','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_feishu_v2_test'],query))
for row in rows:row['source_hash']=mask(row.pop('id'))
result['source_schedules']=rows
network_failures=[]
for service in ['app','worker2']:
 logs=run(compose+['logs','--timestamps','--no-color','--since','2026-10-27T02:44:58Z','--until','2026-10-27T02:45:02Z',service])
 for line in logs.splitlines():
  categories=[v for v in ['unexpected EOF','EOF','connection reset','connection refused','broken pipe','TLS handshake','certificate','x509','no such host','server misbehaving','context canceled','context deadline','request canceled','headers_io_failure','GOAWAY','INTERNAL_ERROR','stream error','http2','i/o timeout','transient','retry'] if v in line]
  if not categories:continue
  at=re.search(r'\d{4}-\d{2}-\d{2}T[0-9:.]+Z',line)
  network_failures.append({'service':service,'at':at[0] if at else None,'categories':categories,'line_digest':hashlib.sha256(line.encode()).hexdigest()[:12]})
result['network_failure_categories']=network_failures[-60:]
episode=[]
for service in ['app','worker2']:
 logs=run(compose+['logs','--timestamps','--no-color','--since','2026-10-27T02:44:59Z','--until','2026-10-27T02:45:01Z',service])
 for line in logs.splitlines():
  if '[FeishuControlTiming] ' not in line:continue
  try:timing=json.loads(line.split('[FeishuControlTiming] ',1)[1])
  except ValueError:continue
  episode.append({'service':service,'started':timing.get('started'),'finished':timing.get('finished'),'failed':timing.get('failed'),
   'stages':timing.get('stages'),'commands':timing.get('commands'),'io':timing.get('io')})
result['incident_control_timings']=episode
'''

CONFIGURE = r'''
if project!='knowhub-feishu-v2-d6e99d894d3a':raise ValueError('isolated_project_required')
if redis('GET',prefix+'trip')!=b'embedding_http_400':raise ValueError('expected_embedding_stop_required')
run(compose+['stop','--timeout','45','app','worker2'])
folder=site/'embedding-batch10-20261027'
folder.mkdir(mode=0o700,exist_ok=True)
if folder.is_symlink():raise ValueError('unsafe_backup_directory')
config=site/'builtin_models.yaml'
raw=config.read_bytes();models=json.loads(raw)
matched=[m for m in models['builtin_models'] if m['id']=='builtin-feishu-v2-embedding' and m['name']=='text-embedding-v4' and m['parameters']['base_url']=='https://uniapi.ruijie.com.cn/v1']
if len(matched)!=1:raise ValueError('exact_gateway_model_required')
backup=folder/'builtin.before.json'
if not backup.exists():backup.write_bytes(raw);backup.chmod(0o600)
matched[0]['parameters'].setdefault('extra_config',{})['embedding_max_batch_size']='10'
config.write_text(json.dumps(models,indent=2)+'\n')
config.chmod(0o644)
selection="SELECT m.id,m.parameters FROM models m JOIN knowledge_bases kb ON kb.embedding_model_id=m.id JOIN users u ON u.id=kb.creator_id WHERE kb.name='test001' AND u.username='test' AND m.name='text-embedding-v4' AND m.parameters->>'base_url'='https://uniapi.ruijie.com.cn/v1'"
original=run(compose+['exec','-T','postgres','psql','-qXAt','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_feishu_v2_test'],"SELECT coalesce(json_agg(t),'[]') FROM ("+selection+") t;").strip()
model_rows=json.loads(original)
if len(model_rows)!=1:raise ValueError('exact_bound_model_required')
model_backup=folder/'model.before.json'
if not model_backup.exists():model_backup.write_text(original);model_backup.chmod(0o600)
row=model_rows[0]
if not re.fullmatch('[a-zA-Z0-9-]{1,100}',row['id']):raise ValueError('invalid_model_identifier')
encoded=__import__('base64').b64encode(json.dumps(row['parameters']).encode()).decode()
query="UPDATE models SET parameters=jsonb_set(parameters::jsonb,'{extra_config}',coalesce(parameters::jsonb->'extra_config','{}'::jsonb)||'{\"embedding_max_batch_size\":\"10\"}'::jsonb) WHERE id='"+row['id']+"' AND parameters::jsonb=convert_from(decode('"+encoded+"','base64'),'UTF8')::jsonb RETURNING id;"
run(compose+['exec','-T','postgres','psql','-qX','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_feishu_v2_test'],query)
result['batch_limit_configuration']={'status':'CONFIGURED','max_batch_size':10,'private_configuration_backup':True,'models_scope':'test001_existing_model','trip_retained':True,'workers_stopped':True}
'''

if __name__ == '__main__':
    parser=argparse.ArgumentParser(add_help=False)
    parser.add_argument('--configure-batch-limit',action='store_true')
    options,remaining=parser.parse_known_args()
    literals=set()
    root=Path(__file__).resolve().parents[2]
    for folder in ['internal/application/service','internal/application/repository']:
        for file in (root/folder).glob('*feishu*.go'):
            if file.name.endswith('_test.go'):continue
            text=file.read_text(encoding='utf-8-sig')
            literals.update(re.findall(r'errors.New\("([^"\n]{12,180})"\)',text))
            literals.update(re.findall(r'reason: "([a-z_]{6,100})"',text))
            literals.update(re.findall(r'legacyFence\("([a-z_]{6,100})"\)',text))
    known=sorted(v for v in literals if 'http' not in v and '%' not in v)
    audit.transport.REMOTE='KNOWN_REASONS='+repr(known)+'\n'+audit.REMOTE.replace('connection.close();print(json.dumps(result))',EXTRA+('\n'+CONFIGURE if options.configure_batch_limit else '')+'\nconnection.close();print(json.dumps(result))')
    sys.argv=[sys.argv[0],*remaining]
    raise SystemExit(audit.transport.main())
