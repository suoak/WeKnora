"""Run the bounded gateway probe inside the verified isolated builder image.

Both SSH and gateway credentials arrive via stdin, never Docker environment or
arguments. The one-shot container has no application volumes or database access.
"""
import base64
import argparse
import json
import sys
from pathlib import Path

import paramiko

NETWORK_PROBE = r'''
import json,ssl,socket,urllib.request,urllib.error
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs): return None
opener=urllib.request.build_opener(NoRedirect(),urllib.request.HTTPSHandler(context=ssl.create_default_context()))
urls=['https://sh.rustup.rs',
 'https://static.rust-lang.org/dist/channel-rust-1.99.1.toml.sha256',
 'https://static.rust-lang.org/rustup/dist/x86_64-unknown-linux-gnu/rustup-init',
 'https://index.crates.io/config.json',
 'https://proxy.golang.org/golang.org/toolchain/@v/v0.0.1-go1.26.9.linux-amd64.info',
 'https://goproxy.cn/golang.org/toolchain/@v/v0.0.1-go1.26.9.linux-amd64.info',
 'https://accounts.feishu.cn/oauth/v3/token']
results=[]
for url in urls:
 row={'url':url}
 try:
  request=urllib.request.Request(url,method='HEAD')
  with opener.open(request,timeout=4) as response: row['http_status']=response.status
 except urllib.error.HTTPError as error: row['http_status']=error.code
 except Exception as error: row['error_class']=type(error).__name__
 results.append(row)
print(json.dumps({'status':'DIAGNOSTIC_ONLY','tls_verification':True,'redirects_followed':False,'probes':results}))
'''

REMOTE = r'''
import base64,json,subprocess,sys
credential=json.loads(sys.stdin.read(16384))
image='sha256:33b84179859a3a275976e22ecf184643e4835de0a230d635f6ad88e299634ef0'
command=['docker','run','--rm','-i','--pull=never','--network','bridge','--read-only',
 '--cap-drop','ALL','--pids-limit','64','--entrypoint','python3',image,'-c',
 "import base64;exec(base64.b64decode('"+PROBE+"'))",'--execute','--batch-size',str(BATCH_SIZE)]
result=subprocess.run(command,input=json.dumps(credential),stdout=subprocess.PIPE,
 stderr=subprocess.PIPE,text=True,timeout=45)
credential.clear()
if not result.stdout.strip():
 print(json.dumps({'status':'FAILED','reason':'remote_gateway_probe_unavailable'}))
else:
 report=json.loads(result.stdout)
 report['execution_location']='isolated_builder_container_on_test_host'
 print(json.dumps(report))
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--network-only', action='store_true')
    parser.add_argument('--batch-size', type=int, choices=range(1, 21), default=1)
    args = parser.parse_args()
    client = paramiko.SSHClient()
    try:
        credential = json.loads(sys.stdin.read(32768))
        if credential.get("host") != "172.30.232.42" or credential.get("user") != "aqyfzx":
            raise ValueError("unexpected_target")
        gateway = {} if args.network_only else credential.pop("gateway")
        client.load_system_host_keys()
        known = Path.home() / ".ssh" / "known_hosts"
        if known.is_file():
            client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential["host"], port=22, username=credential["user"], password=credential["password"],
                       timeout=15, auth_timeout=15, banner_timeout=15, allow_agent=False, look_for_keys=False)
        credential.clear()
        probe = NETWORK_PROBE.encode() if args.network_only else Path(__file__).with_name("feishu_v2_gateway_probe.py").read_bytes()
        prefix = "BATCH_SIZE=" + repr(args.batch_size) + "\nPROBE=" + repr(base64.b64encode(probe).decode()) + "\n"
        program = base64.b64encode((prefix + REMOTE).encode()).decode()
        incoming, out, _ = client.exec_command(
            "python3 -c \"import base64;exec(base64.b64decode('" + program + "'))\"", timeout=60)
        incoming.write(json.dumps(gateway))
        incoming.channel.shutdown_write()
        gateway.clear()
        raw = out.read(65536)
        if out.channel.recv_exit_status() != 0:
            raise ValueError("remote_probe_failed")
        report = json.loads(raw)
        print(json.dumps(report))
        return 0 if report.get("status") in ("PASS", "DIAGNOSTIC_ONLY") else 1
    except (OSError, ValueError, KeyError, TypeError, paramiko.SSHException):
        print(json.dumps({"status": "FAILED", "reason": "verified_remote_gateway_probe_unavailable"}))
        return 2
    finally:
        client.close()


if __name__ == "__main__":
    sys.exit(main())
