"""Bounded local checks with exact argv, SHA256 inputs and original output."""
from pathlib import Path
import datetime,hashlib,json,subprocess,sys
ROOT=Path(__file__).resolve().parents[4];OUT=ROOT/'docs/integration/knowhub-mcp-portal/evidence/batch7/published/local';OUT.mkdir(parents=True,exist_ok=True)
phase=sys.argv[1];stamp=datetime.datetime.now().strftime('%Y%m%d-%H%M%S');results=[]
commands={
 'frontend':[
 ('frontend-tests',['node','node_modules/tsx/dist/cli.mjs','--test','src/components/portal/publicationWorkflow.test.ts','src/views/integrations/personalMcpConfig.test.ts','src/views/integrations/mcpConfig.test.ts','src/views/integrations/mcpAccessEditor.test.ts','src/views/integrations/mcpAccessLifecycle.test.ts','src/views/integrations/mcpAccessCenter.test.ts','src/views/integrations/mcpAccessScope.test.ts','src/views/portal/portalContracts.test.ts']),
 ('frontend-types',['node','node_modules/vue-tsc/bin/vue-tsc.js','--build','--pretty','false']),
 ('frontend-build',['node','node_modules/vite/bin/vite.js','build','--configLoader','runner','--outDir','E:/AIData/Codex/knowhub-b7c-frontend-'+stamp])],
 'windows-python':[('windows-python-full',['E:/AIData/CodexTemp/knowhub25-personal-f1-20261010/venv/Scripts/python.exe','-m','unittest','discover','-s','mcp-server','-p','test_*.py','-v'])]
}[phase]
for name,argv in commands:
 cwd=ROOT/'frontend' if phase=='frontend' else ROOT
 import os
 env=dict(os.environ,PYTHONUTF8='1',WEKNORA_API_KEY='')
 start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (OUT/(stamp+'-'+name+'.log')).open('wb') as f:
  try:code=subprocess.run(argv,cwd=cwd,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=600).returncode
  except subprocess.TimeoutExpired:code=124
 inputs={str(p.relative_to(ROOT)).replace('\\','/'):hashlib.sha256(p.read_bytes()).hexdigest() for p in list((ROOT/'frontend/src').rglob('*.vue'))+list((ROOT/'frontend/src').rglob('*.ts'))+list((ROOT/'frontend/tests').glob('*')) if p.is_file()} if phase=='frontend' else {str(p.relative_to(ROOT)).replace('\\','/'):hashlib.sha256(p.read_bytes()).hexdigest() for p in (ROOT/'mcp-server').glob('*.py')}
 row={'name':name,'argv':argv,'cwd':str(cwd),'started_utc':start,'exit_code':code,'log':stamp+'-'+name+'.log','inputs':inputs};results.append(row);print(json.dumps({'name':name,'exit_code':code}),flush=True)
 (OUT/(stamp+'-'+phase+'-results.json')).write_text(json.dumps(results,indent=2),encoding='utf8')
