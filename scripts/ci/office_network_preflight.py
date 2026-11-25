"""Read-only routing and Docker-network inventory after default-pool exhaustion."""
from office_test_remote import connect,execute
CODE=r'''
import json,os,socket,subprocess
assert socket.gethostname()=='aqyfzx10' and os.geteuid()==1000
def run(args):return subprocess.check_output(args,text=True)
ids=run(['docker','network','ls','-q']).split()
networks=json.loads(run(['docker','network','inspect',*ids]))
print(json.dumps({'routes':json.loads(run(['ip','-j','route','show','table','all'])),
 'networks':[{'name':x['Name'],'driver':x['Driver'],'ipam':x['IPAM']} for x in networks],
 'own_resources':run(['docker','ps','-a','--filter','label=com.docker.compose.project=knowhub-office-guard-test','--format','{{.Names}} {{.Status}}']),
 'own_volumes':run(['docker','volume','ls','--filter','label=com.docker.compose.project=knowhub-office-guard-test','--format','{{.Name}}'])}))
'''
if __name__=='__main__':
    with connect() as client:raise SystemExit(execute(client,CODE))
