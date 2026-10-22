"""Re-run the entire PG gate with its unchanged safe output-path assertion."""
from pathlib import Path
import json,subprocess,sys,time
base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-mcp-v1-20261021')
m=json.loads((base/'manifest.json').read_text());assert base.name==m['candidate_sha256'][:16]
out=base/'results/pg-recheck';out.mkdir(exist_ok=True);assert not(out/'results.json').exists();results=[]
deadline=time.monotonic()+2400
while not any(r['name']=='service-full'and r['exit_code']==0 for r in json.loads((base/'results/acceptance/results.json').read_text())):
 assert time.monotonic()<deadline,'prior suite must finish before preserving its synthetic databases'
 time.sleep(2)
instance=json.loads((base/'results/deploy/instance.json').read_text());project=instance['project']
assert project=='knowhub-v1-'+m['candidate_sha256'][:12]
compose=['docker','compose','--project-name',project,'--project-directory',str(base),'-f',str(base/'compose.yml')]
def run(name,argv):
 with(out/(name+'.log')).open('wb')as f:code=subprocess.run(argv,stdout=f,stderr=subprocess.STDOUT,timeout=1500).returncode
 results.append({'name':name,'argv':argv,'exit_code':code});(out/'results.json').write_text(json.dumps(results,indent=2));(out/(name+'.exit.txt')).write_text(str(code));assert code==0,name
# Keep the first attempt's complete synthetic DBs, rather than deleting evidence.
for name in ['knowhub_m6','knowhub_m7','knowhub_m7_dynamic','knowhub_v1_dimensions']:
 run('preserve-'+name,compose+['exec','-T','postgres','psql','-v','ON_ERROR_STOP=1','-U','postgres','-d','postgres','-c','ALTER DATABASE '+name+' RENAME TO '+name+'_attempt1'])
run('fresh-base',compose+['exec','-T','postgres','psql','-v','ON_ERROR_STOP=1','-U','postgres','-d','postgres','-c','CREATE DATABASE knowhub_m6'])
argv=next(r['argv']for r in json.loads((base/'results/acceptance/results.json').read_text())if r['name']=='postgres-security-scale')
argv=[x.replace('KNOWHUB_M7_PLAN_DIR=/artifacts/pg-plans','KNOWHUB_M7_PLAN_DIR=/work/results/pg-plans-retry')for x in argv]
argv[3:3]=['-v',str(base/'artifacts')+':/work/results']
run('postgres-security-scale-full-retry',argv)
(out/'done.json').write_text(json.dumps({'candidate_sha256':m['candidate_sha256'],'all_passed':True,'assertions_changed':False,'original_failure_retained':True,'reason':'test runner originally supplied a plan directory outside the required /work/results/ prefix'}))
