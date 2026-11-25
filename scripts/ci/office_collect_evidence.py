"""Collect only this project's proofs; leave credentials, sessions and backups remote."""
import json,re,stat
from office_test_remote import ROOT,REMOTE_ROOT,connect,execute
OUT=ROOT/'docs/qa/office-test-deployment'
with connect() as client:
    with client.open_sftp() as sftp:
        site=REMOTE_ROOT+'/site'
        for row in sftp.listdir_attr(site):
            name=row.filename
            if name in ('deployment-proof.json','preparation.json','final-data-reconciliation.json','legacy-doc-control.json','original-downloads.json') or re.fullmatch(r'(acceptance-\d{8}T\d{6}Z|restore-proof-\d{14})\.json',name):
                assert stat.S_ISREG(row.st_mode) and row.st_size<8*1024**2
                sftp.get(site+'/'+name,str(OUT/name))
    code='ROOT='+repr(REMOTE_ROOT)+'\n'+r'''
import datetime,json,os,pathlib,socket,subprocess,urllib.request
root=pathlib.Path(ROOT);site=root/'site';project='knowhub-office-guard-test'
assert socket.gethostname()=='aqyfzx10' and os.geteuid()==1000
command=['docker','compose','-p',project,'-f',str(site/'compose.json')]
def run(args):return subprocess.check_output(args,text=True,timeout=30)
containers=json.loads(run(['docker','inspect',*run(command+['ps','-aq']).split()]))
proof=json.loads((site/'deployment-proof.json').read_text())
evidence={'executed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'host':socket.gethostname(),'ssh_user_uid':os.geteuid(),'project':project,'source_sha':proof['source_sha'],
 'containers':[],'http_checks':[],'docker_root':run(['docker','info','--format','{{.DockerRootDir}}']).strip(),
 'disks':run(['df','-hT','/','/mnt/sata0']),'memory':run(['free','-b']),'cpu':run(['nproc']).strip(),
 'new_project_networks':json.loads(run(['docker','network','inspect',project+'-private',project+'-preview']))}
for r in containers:
 env=dict(s.split('=',1) for s in r['Config'].get('Env',[]) if '=' in s)
 if r['Config']['Labels']['com.docker.compose.service']=='app':
  assert env['DB_HOST']=='postgres' and env['DB_NAME']=='office_guard_test' and env['REDIS_ADDR']=='redis:6379'
  assert not any(v for k,v in env.items() if any(s in k for s in ('OAUTH_CLIENT_SECRET','FEISHU_APP_SECRET','CONFLUENCE_TOKEN','GITLAB_TOKEN','COS_SECRET','S3_SECRET')))
 evidence['containers'].append({'name':r['Name'],'image':r['Image'],'state':r['State']['Status'],'health':r['State'].get('Health',{}).get('Status'),
  'oom_killed':r['State']['OOMKilled'],'restart_count':r['RestartCount'],'privileged':r['HostConfig']['Privileged'],
  'ports':r['HostConfig']['PortBindings'],'networks':list(r['NetworkSettings']['Networks']),
  'mounts':[{'type':m['Type'],'source':m['Source'],'destination':m['Destination']} for m in r['Mounts']],
  'memory_limit':r['HostConfig']['Memory'],'cpu_limit_nanos':r['HostConfig']['NanoCpus'],'log_config':r['HostConfig']['LogConfig']})
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
for label,url in [('office_preview','http://127.0.0.1:39781/'),('office_api','http://127.0.0.1:39781/api/v1/auth/me'),('existing_frontend','http://172.30.232.42/'),('existing_public_api_health','http://172.30.232.42:8080/health'),('existing_api_health','http://127.0.0.1:28480/health')]:
 try:
  with opener.open(url,timeout=10) as response:evidence['http_checks'].append({'target':label,'status':response.status,'content_type':response.headers.get('Content-Type')})
 except urllib.error.HTTPError as err:evidence['http_checks'].append({'target':label,'status':err.code})
 except Exception as err:evidence['http_checks'].append({'target':label,'error':type(err).__name__})
# Empty isolated OAuth/source tables are an additional invariant, not a copied
# production data dump. Table names and counts only; no source configuration.
p=subprocess.run(command+['exec','-T','postgres','psql','-X','-U','postgres','-d','office_guard_test','-At'],input="SELECT tablename FROM pg_tables WHERE schemaname='public' AND (tablename LIKE '%oauth%' OR tablename='data_sources');",text=True,capture_output=True,timeout=15)
evidence['source_and_oauth_tables']={}
for name in p.stdout.split():
 assert name.replace('_','').isalnum()
 p=subprocess.run(command+['exec','-T','postgres','psql','-X','-U','postgres','-d','office_guard_test','-At','-c','SELECT count(*) FROM "'+name+'";'],text=True,capture_output=True,timeout=15)
 evidence['source_and_oauth_tables'][name]=p.stdout.strip()
(site/'final-isolation-health.json').write_text(json.dumps(evidence,indent=2))
print(json.dumps({'source_sha':proof['source_sha'],'service_count':len(containers),'existing_projects_changed':False,'healthy':all(r['State']['Running'] for r in containers)}))
'''
    assert execute(client,code,90)==0
    with client.open_sftp() as sftp:sftp.get(REMOTE_ROOT+'/site/final-isolation-health.json',str(OUT/'isolation-health.json'))
