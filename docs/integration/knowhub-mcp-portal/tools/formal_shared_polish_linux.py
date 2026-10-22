"""Full frontend regression in a complete source copy; frozen source untouched."""
from pathlib import Path
import hashlib,json,subprocess,sys
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
state=json.loads((base/'private/state.json').read_text());candidate=Path(state['candidate']);m=json.loads((candidate/'manifest.json').read_text());out=base/'results/polish';out.mkdir(exist_ok=False);rows=[]
patch=base/'polish-MCPAccessKeys.vue';sha=hashlib.sha256(patch.read_bytes()).hexdigest();assert sha=='614cdec90ac79da12636a21825b84515727905484404c2f275acb9333c7e9dbc'
volume='knowhub-v1-wiki-dynamic-dev-'+m['candidate_sha256'][:16]
common=['docker','run','--rm','--network','none','-v',volume+':/v1','-v',str(candidate/'source')+':/frozen:ro','-v',str(patch)+':/patch.vue:ro','-e','NODE_OPTIONS=--max-old-space-size=4096','-w','/v1/formal-polish/frontend','node:22-bookworm']
def run(name,args,timeout=1200):
 with(out/(name+'.log')).open('wb')as f:
  try:code=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
  except subprocess.TimeoutExpired:code=124
 rows.append({'name':name,'argv':args,'exit_code':code});(out/'commands.json').write_text(json.dumps(rows,indent=2));return code
assert run('prepare-full-source',common+['sh','-c','mkdir -p /v1/formal-polish; cp -a /frozen/. /v1/formal-polish/; cp /patch.vue src/views/integrations/MCPAccessKeys.vue; ln -s /v1/frontend/node_modules node_modules'])==0
run('frontend-full',common+['npm','test'])
run('frontend-types',common+['npm','run','type-check'])
run('frontend-build',common+['npm','run','build','--','--configLoader','runner'])
result={'exit_code':0 if all(x['exit_code']==0 for x in rows)else 1,'base_candidate_sha256':m['candidate_sha256'],'patch_sha256':sha,'tests_and_baselines_unchanged':True,'complete_parent_source_present':True,'commands':[{k:x[k]for k in('name','exit_code')}for x in rows]}
(out/'result.json').write_text(json.dumps(result,indent=2));print(json.dumps(result));sys.exit(result['exit_code'])
