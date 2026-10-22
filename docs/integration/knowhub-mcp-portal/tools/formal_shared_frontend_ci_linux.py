"""Run all frontend tests on the immutable candidate's build workspace."""
from pathlib import Path
import datetime,json,subprocess,sys
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
state=json.loads((base/'private/state.json').read_text());candidate=Path(state['candidate']);m=json.loads((candidate/'manifest.json').read_text());out=base/'results/frontend-ci';out.mkdir(exist_ok=False)
volume='knowhub-v1-wiki-dynamic-dev-'+m['candidate_sha256'][:16]
args=['docker','run','--rm','--network','none','-v',volume+':/v1','-e','NODE_OPTIONS=--max-old-space-size=4096','-w','/v1/frontend','node:22-bookworm','npm','test']
with(out/'all-frontend-tests.log').open('wb')as f:
 try:code=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=1200).returncode
 except subprocess.TimeoutExpired:code=124
r={'exit_code':code,'argv':args,'candidate_sha256':m['candidate_sha256'],'scope':'full npm test, not only personal MCP test selection','runtime':'Node22 same as deployed candidate build; GitHub workflow Node24 must independently pass'}
(out/'result.json').write_text(json.dumps(r,indent=2));print(json.dumps(r));sys.exit(code)
