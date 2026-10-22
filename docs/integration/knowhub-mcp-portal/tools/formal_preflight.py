"""Pinned SSH read-only preflight. Never modifies remote files or services."""
from pathlib import Path
import base64,datetime,hashlib,importlib.util,json,logging,shlex,sys
import paramiko
from v1_host import connect

root=Path(__file__).resolve().parents[4];docs=root/'docs/integration/knowhub-mcp-portal'
mode=sys.argv[1];assert mode in ('test','test-active','production')
kind=sys.argv[2]if len(sys.argv)>2 else'base';assert kind in ('base','supplement')
out=docs/'evidence/formal-acceptance';out.mkdir(exist_ok=True)
stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
payload=(Path(__file__).parent/('formal_preflight_remote.py'if kind=='base'else'formal_preflight_supplement.py')).read_text(encoding='utf8')
wrapper="import json,sys,traceback\ntry:\n exec(compile("+repr(payload)+",'<readonly-preflight>','exec'))\nexcept Exception as e:\n result=globals().get('out',{});result.update({'status':'BLOCKED_PARTIAL','error_class':type(e).__name__,'failed_line':traceback.extract_tb(e.__traceback__)[-1].lineno});print(json.dumps(result,indent=2));sys.exit(1)\n"
client=None
try:
    if mode!='production':client=connect()
    else:
        spec=importlib.util.spec_from_file_location('crypto','E:/AIData/Codex/sourceid-ssh.py');crypto=importlib.util.module_from_spec(spec);spec.loader.exec_module(crypto)
        host='10.51.134.114';keys=paramiko.HostKeys('E:/AIData/Codex/sourceid-production-host-public-key.txt');key=keys.lookup(host)['ssh-ed25519']
        assert 'SHA256:'+base64.b64encode(hashlib.sha256(key.asbytes()).digest()).decode().rstrip('=')=='SHA256:u7TLDQSRo5z7oGMjNYsPfENIlbXGI7KdslJCajR57qA'
        logging.getLogger('paramiko').disabled=True;client=paramiko.SSHClient();client.get_host_keys().add(host,'ssh-ed25519',key);client.set_missing_host_key_policy(paramiko.RejectPolicy())
        password=crypto.crypt(Path('E:/AIData/Codex/secure/sourceid-production-host.ssh.dpapi').read_bytes(),False).decode()
        client.connect(host,username='aqyfzx',password=password,allow_agent=False,look_for_keys=False,timeout=15);password=None
    i,o,e=client.exec_command(shlex.join(['python3','-',mode]),timeout=180)
    i.write(wrapper);i.channel.shutdown_write();raw=o.read();code=o.channel.recv_exit_status()
    result=json.loads(raw);result['ssh_exit_code']=code;result['inspection_script_sha256']=hashlib.sha256(payload.encode()).hexdigest()
except Exception as e:
    result={'observed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'mode':mode,'status':'BLOCKED','error_class':type(e).__name__,'remote_writes':False}
finally:
    if client:client.close()
path=out/(mode+'-preflight-'+('supplement-'if kind=='supplement'else'')+stamp+'.json');assert not path.exists();path.write_text(json.dumps(result,indent=2),encoding='utf8')
print(json.dumps({'evidence':str(path),'mode':mode,'status':result.get('status','COLLECTED'),'ssh_exit_code':result.get('ssh_exit_code'),'schema_version':result.get('schema_version'),'portal_counts':result.get('portal_counts'),'error_class':result.get('error_class'),'failed_line':result.get('failed_line'),'services_observed':len(result.get('services',[]))}))
