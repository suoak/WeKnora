"""Run the Go commands used by App CI before publishing the feature branch."""
from pathlib import Path
import datetime,json,subprocess,sys
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
dev=Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/090216bc1a7d0c54')
out=base/'results/ci-preflight';out.mkdir(exist_ok=False);rows=[]
prior=json.loads((dev/'results/results.json').read_text());go=next(x['argv']for x in prior if x['name']=='full-go');go=go[:go.index('go')];go[go.index('--name')+1]+='-ci-preflight'
def run(name,cmd):
 row={'name':name,'argv':go+cmd,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
 with(out/(name+'.log')).open('wb')as f:
  try:row['exit_code']=subprocess.run(row['argv'],stdout=f,stderr=subprocess.STDOUT,timeout=1500).returncode
  except subprocess.TimeoutExpired:row['exit_code']=124
 rows.append(row);(out/'commands.json').write_text(json.dumps(rows,indent=2))
run('go-vet-default',['go','vet','./...'])
run('go-test-default',['go','test','-p','2','-count=1','./...'])
run('go-test-desktop-container',['go','test','-tags','desktop','./internal/container'])
result={'exit_code':0 if all(x['exit_code']==0 for x in rows)else 1,'runtime_source_sha256':'090216bc1a7d0c547235df30eef6f425050b9260fadba9791329b6c267a24783','commands':[{k:x[k]for k in('name','exit_code')}for x in rows]}
(out/'result.json').write_text(json.dumps(result,indent=2));print(json.dumps(result));sys.exit(result['exit_code'])
