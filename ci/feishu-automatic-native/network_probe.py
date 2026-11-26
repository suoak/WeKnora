"""Read-only transport diagnostics before authenticated functional requests."""
import base64,json,os,pathlib,subprocess,urllib.request,urllib.error
ROOT=pathlib.Path(os.environ['QUALIFICATION_ROOT']);PROJECT='knowhub-auto-native-'+os.environ['GITHUB_RUN_ID'];C=['docker','compose','-p',PROJECT,'-f',str(ROOT/'compose.json')]
OUT=pathlib.Path(os.environ['GITHUB_WORKSPACE'])/'evidence';OUT.mkdir(exist_ok=True)
cid=subprocess.check_output(C+['ps','-q','app'],text=True).strip();app=json.loads(subprocess.check_output(['docker','inspect',cid],text=True))[0]
assert app['Config']['Labels']['com.docker.compose.project']==PROJECT
host={'url':'http://127.0.0.1:39881/health'}
try:
 with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(host['url'],timeout=5) as response:host['http_status']=response.status
except urllib.error.HTTPError as error:host['http_status']=error.code
except urllib.error.URLError as error:host.update({'error_reason_class':type(error.reason).__name__,'errno':getattr(error.reason,'errno',None)})
code=pathlib.Path(__file__).with_name('http_driver.py').read_text()
packet={'method':'GET','path':'/health','headers':{},'payload':None}
proc=subprocess.run(C+['exec','-T','--user','1000:1000','app','python3','-c',code],input=json.dumps(packet).encode(),capture_output=True,timeout=100)
inside={'client_uid':1000,'url':'http://127.0.0.1:8080/health','process_exit':proc.returncode}
if proc.returncode==0:
 result=json.loads(proc.stdout);inside['http_status']=result['status']
 if result.get('transport_error'):inside.update(result['transport_error'])
mode='host_loopback' if host.get('http_status')==200 else 'own_app_container_http' if inside.get('http_status')==200 and host.get('errno') in [111,101,113] else 'unreachable'
report={'source_sha':os.environ['SOURCE_SHA'],'host_probe':host,'container_probe':inside,'actual_hostport_mapping':app['NetworkSettings']['Ports'].get('8080/tcp'),'internal_network':True,'selected_client_transport':mode,'authentication_unchanged':True,'direct_go_invocation':False}
(ROOT/'transport.json').write_text(json.dumps(report));os.chmod(ROOT/'transport.json',0o600)
(OUT/'network-probe.json').write_text(json.dumps(report,indent=2));print(json.dumps(report));assert mode!='unreachable','Neither permitted real HTTP route reached own APP'
