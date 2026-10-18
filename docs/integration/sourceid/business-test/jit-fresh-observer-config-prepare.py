"""Prepare inactive whitelist-only gateway logging; never install/reload it."""
import os,sys,ipaddress
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
sys.path.insert(0,str(B/'monitor/business-test'))
from company_gateway_observer import configuration
source=str(ipaddress.ip_address(os.environ['SSH_CONNECTION'].split()[0]))
gateway=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/entry/nginx.conf');before=gateway.read_bytes()
candidate=(R/'private/route/candidate.conf').read_text()
updated=configuration(candidate,source,[],[],None)
old='map $uri $kh_kind { default ""; '
assert updated.count(old)==1
extra='"/api/v1/auth/oidc/result" oidc_result; "/api/v1/auth/oidc/callback" oidc_callback; "/api/v1/auth/oidc/config" oidc_config; '
updated=updated.replace(old,old+extra,1)
assert '$request_uri' not in updated and '$http_cookie' not in updated and '$http_authorization' not in updated and '$request_body' not in updated
if 'worker_shutdown_timeout ' not in updated:updated='worker_shutdown_timeout 5s;\n'+updated
path=R/'private/route/candidate-observed.conf';path.write_text(updated);path.chmod(0o600)
run(['docker','run','--rm','--network','knowhub-jit-fresh_isolated','--log-driver','none','--mount','type=bind,source='+str(path)+',target=/etc/nginx/nginx.conf,readonly','--entrypoint','nginx',inspect('knowhub-company-entry-entry-1','.Image'),'-t'],timeout=30)
assert gateway.read_bytes()==before
print(json.dumps({'result':'INACTIVE_OBSERVER_CONFIG_SYNTAX_PASS','loaded_on_gateway':False,'business_gates_changed':False,'real_sid_auth_requests':0,'whitelist_only':True,'query_headers_body_logging':False,'worker_shutdown_seconds':5}))
