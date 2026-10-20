"""Retry acceptance only after stopped isolated harness failure; preserve evidence."""
import sys
sys.path.insert(0,'/home/aqyfzx/ai/knowhub-legacy-clean-target-20261016')
from support import *
p=BASE/'acceptance-result.json';old=json.loads(p.read_text());assert old['result']=='FAIL'
# Never replace migration/snapshot or an active harness.
assert not subprocess.run(['docker','inspect','knowhub-legacy-clean-20261016-app'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0
p.rename(BASE/('acceptance-failed-'+str(time.time_ns())+'.json'))
with open(os.devnull,'wb') as sink:
 process=subprocess.Popen(['python3',str(BASE/'clean-target-acceptance-worker.py')],stdout=sink,stderr=sink,start_new_session=True)
print(json.dumps({'isolated_acceptance_retry_started':True,'pid':process.pid,'live_changes':False}))
