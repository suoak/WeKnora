"""Backup/restore only the newly created Office test PostgreSQL instance."""
from office_test_remote import connect,execute,REMOTE_ROOT
CODE='ROOT='+repr(REMOTE_ROOT)+'\n'+r'''
import datetime,hashlib,json,os,pathlib,re,socket,subprocess
root=pathlib.Path(ROOT);site=root/'site';project='knowhub-office-guard-test'
assert socket.gethostname()=='aqyfzx10' and os.geteuid()==1000 and root.resolve()==root
command=['docker','compose','-p',project,'-f',str(site/'compose.json')]
cid=subprocess.check_output(command+['ps','-q','postgres'],text=True).strip()
row=json.loads(subprocess.check_output(['docker','inspect',cid],text=True))[0]
assert row['Config']['Labels']['com.docker.compose.project']==project
assert all(project in m['Name'] for m in row['Mounts'] if m['Type']=='volume')
def sql(db,q):
 p=subprocess.run(command+['exec','-T','postgres','psql','-X','-U','postgres','-d',db,'-At','-v','ON_ERROR_STOP=1'],input=q,text=True,capture_output=True,timeout=60)
 assert p.returncode==0,p.stderr[-1000:]
 return p.stdout.strip()
stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%d%H%M%S')
backup=site/('office-only-'+stamp+'.dump');target='office_guard_restore_'+stamp
assert re.fullmatch(r'office_guard_restore_[0-9]{14}',target)
with backup.open('xb') as out:
 p=subprocess.run(command+['exec','-T','postgres','pg_dump','-U','postgres','-d','office_guard_test','-Fc'],stdout=out,stderr=subprocess.PIPE,timeout=120)
 assert p.returncode==0,p.stderr.decode()[-1000:]
backup.chmod(0o600)
sql('postgres','CREATE DATABASE '+target+' TEMPLATE template0;')
with backup.open('rb') as data:
 p=subprocess.run(command+['exec','-T','postgres','pg_restore','--exit-on-error','-U','postgres','-d',target],stdin=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=180)
 assert p.returncode==0,p.stderr.decode()[-2000:]
tables=json.loads(sql(target,"SELECT json_agg(tablename) FROM pg_tables WHERE schemaname='public' AND (tablename IN ('knowledges','chunks') OR tablename LIKE 'embeddings%');"))
counts={}
for name in tables:
 assert re.fullmatch('[a-zA-Z0-9_]+',name)
 counts[name]={'live':int(sql('office_guard_test','SELECT count(*) FROM "'+name+'";')),'restored':int(sql(target,'SELECT count(*) FROM "'+name+'";'))}
assert all(v['live']==v['restored'] for v in counts.values())
result={'result':'PASS','project':project,'backup':str(backup),'bytes':backup.stat().st_size,'sha256':hashlib.sha256(backup.read_bytes()).hexdigest(),
 'restore_database':target,'counts':counts,'existing_databases_modified':False,'retention':'Retain this test recovery point and predecessor image until acceptance/review completes; no historic backups deleted.'}
fd=os.open(site/('restore-proof-'+stamp+'.json'),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
with os.fdopen(fd,'w') as out:json.dump(result,out,indent=2)
print(json.dumps(result))
'''
if __name__=='__main__':
 with connect() as client:raise SystemExit(execute(client,CODE,450))
