"""Launch isolated candidate acceptance; no live service modification."""
import sys
sys.path.insert(0,'/home/aqyfzx/ai/knowhub-legacy-clean-target-20261016')
from support import *
assert json.loads((BASE/'preparation-result.json').read_text())['result']=='PASS'
assert not (BASE/'acceptance-result.json').exists()
with open(os.devnull,'wb') as sink:
 p=subprocess.Popen(['python3',str(BASE/'clean-target-acceptance-worker.py')],stdout=sink,stderr=sink,start_new_session=True)
print(json.dumps({'candidate_acceptance_started':True,'pid':p.pid,'live_changes':False}))
