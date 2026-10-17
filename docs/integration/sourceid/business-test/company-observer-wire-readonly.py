"""One unauthenticated gateway probe, restore exact config in finally."""
import sys,subprocess,select,os,time,json,uuid
from pathlib import Path
ROOT=Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');sys.path.insert(0,str(ROOT/'monitor-company-first/business-test'))
from company_business_observer import ServerBusinessObserver
from company_gateway_observer import GatewayObservation,CONFIG,ENTRY
from business_monitor_pilot import command,inspect
source=os.environ['SSH_CONNECTION'].split()[0];trace=str(uuid.uuid4());sample=['aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa','bbbbbbbb-bbbb-4bbb-abbb-bbbbbbbbbbbb','cccccccc-cccc-4ccc-accc-cccccccccccc']
observer=ServerBusinessObserver(source,[sample[0]],[sample[1]],sample[2]);guard=GatewayObservation(command,inspect,source,[],[],None)
original=CONFIG.read_bytes();p=None;matched=False;buffer=b'';restored=False
try:
 guard.enable()
 p=subprocess.Popen(['docker','--host','unix:///var/run/docker.sock','attach','--no-stdin','--sig-proxy=false',ENTRY],stdout=subprocess.PIPE,stderr=subprocess.STDOUT);os.set_blocking(p.stdout.fileno(),False)
 time.sleep(1);print(json.dumps({'phase':'READY','probe_request_id':trace}),flush=True)
 end=time.monotonic()+20
 while time.monotonic()<end and len(buffer)<262144:
  if select.select([p.stdout],[],[],.2)[0]:
   data=os.read(p.stdout.fileno(),65536)
   if not data:break
   buffer+=data
   while b'\n' in buffer:
    line,buffer=buffer.split(b'\n',1)
    if len(line)>4096:continue
    observer.observe(line.decode('utf-8','replace'),True)
    if observer.failed:matched=True
   if matched:break
finally:
 if p:
  p.terminate()
  try:p.wait(timeout=5)
  except subprocess.TimeoutExpired:p.kill();p.wait()
  p.stdout.close()
 guard.restore();restored=CONFIG.read_bytes()==original;buffer=None
out={'result':'PASS' if matched and restored else 'FAIL','app_project':'knowhub-company-test','channel':'CURRENT_GATEWAY_METADATA_TO_BUSINESS_OBSERVER','unauthenticated_401_parsed':matched,'gateway_configuration_restored_byte_identical':restored,'app_log_level_changed':False,'site_redeployed':False,'browser_profile_changed':False,'cookie_or_token_read':False,'raw_logs_saved':False,'real_sid_requests':0,'app_recreated':False,'business_gates_changed':False}
print(json.dumps(out),flush=True)
