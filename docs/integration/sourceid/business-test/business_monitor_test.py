"""Synthetic control/HTTP-channel calibration; never a real SID request."""
import contextlib
import ast
import copy
import http.server
import io
import json
import threading
import unittest
import urllib.error
import urllib.request
from unittest.mock import patch
from pathlib import Path
import export_token_diagnostics

from business_monitor import BusinessMonitor, COMMON, EXISTING, JIT, IMAGE, PROJECT, REVISION, Transaction, finish
from business_monitor_adapter import CLOSED, run, PilotBusinessAdapter
from business_monitor_pilot import main, DockerPilotPort, ROOT, RELEASE, ENTRY, FRONT_IMAGE

A='aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa';C='cccccccc-cccc-4ccc-accc-cccccccccccc'
OTHER='bbbbbbbb-bbbb-4bbb-abbb-bbbbbbbbbbbb'
PRIVATE='SYNTHETIC_PRIVATE_TRANSACTION_HANDLE_NOT_FOR_EXPORT'
SESSION='SYNTHETIC_RANDOM_SESSION_NOT_FOR_EXPORT'
POISON='SYNTHETIC_PASSWORD_CODE_COOKIE_EMAIL_SECRET_NEVER_SAVE'


def event(stage,passed=True,trace=C,category='none'):
    return {'event':'sourceid_stage','trace':trace,'stage':stage,'pass':passed,'duration_ms':1,'category':category}


def token_http(attempts=1):
    # Synthetic fixture generated from the reviewed whitelist's closed schema.
    tree=ast.parse(Path(export_token_diagnostics.rules.__file__).read_text(encoding='utf-8'))
    fn=next(x for x in tree.body if isinstance(x,ast.FunctionDef) and x.name=='transport')
    constants={x.targets[0].id:ast.literal_eval(x.value) for x in fn.body if isinstance(x,ast.Assign)}
    t={k:sorted(v)[0] for k,v in constants['enums'].items()}
    t.update({k:False for k in constants['bools']});t.update({k:1 for k in constants['ints']})
    t.update(trace=C,utc='2026-10-17T00:00:00Z',beijing='2026-10-17T08:00:00+08:00',remote_address='127.0.0.1:8443',response_date_utc='NOT_CAPTURED',known_body_field_counts={},network_events=[],observed_write_attempts=attempts,retry_observed=attempts>1)
    return {'event':'sourceid_token_http','trace':C,'http':[{'stage':'token','status':200,'duration_ms':1,'transport':t}]}


class MockPort:
    """Mock SID stages + synthetic DB/browser observations, virtual clock."""
    def __init__(self,case='existing',failure=None,role='viewer'):
        self.case=case;self.failure=failure;self.role=role;self.clock=100.;self.step=0
        self.flags=dict(CLOSED);self.enabled=False;self.saved=[];self.order=[];self.revoked=[]
        self.session=None;self.closed=False;self.snapshot_marker=object();self.browser_source=None
        self.ready_seen=False
    def now(self):return self.clock
    def pause(self):self.step+=1;self.clock+=1
    def inventory(self):
        return {'project':PROJECT,'image':IMAGE,'revision':REVISION,'schema':3029,'dirty':False,'healthy':True,'flags':dict(self.flags)}
    def snapshot(self):return {'other_services':self.other_services(),'private':self.snapshot_marker}
    def save_private_baseline(self,b):self.before=b
    def other_services(self):return {'frontend':'unchanged','postgres':'unchanged','redis':'unchanged','docreader':'unchanged'}
    def transactions(self):
        if not self.ready_seen:return []
        if self.failure=='timeout' and self.step<600:return [Transaction(PRIVATE,100,110,False,False)]
        rows=[Transaction(PRIVATE,100,700,self.step>=1,False)]
        if self.failure=='second_authorize' and self.step>=1:rows.append(Transaction('OTHER_PRIVATE',100,700,False,False))
        return rows
    def enable_approved(self,case):
        self.order.append('enable');self.enabled=True
        self.flags.update(OIDC_AUTH_ENABLE='true',SOURCEID_PROTOCOL_ONLY='false',SOURCEID_CONTRACT_CONFIRMED='true',SOURCEID_EMAIL_BINDING_ENABLE='true',SOURCEID_AUTO_BINDING_ENABLE='true',SOURCEID_SESSION_MODE='local_approved',SOURCEID_ACCOUNT_SCOPE='enterprise_all',SOURCEID_ADMISSION_MODE='oidc_sso_approved',SOURCEID_DIAGNOSTICS_ENABLE='true',SOURCEID_JIT_ENABLE='true' if case=='jit' else 'false')
        if self.failure=='partial_enable':raise RuntimeError(POISON)
    def start_stream(self):self.order.append('attach')
    def ready(self,run_id):self.run_id=run_id;self.ready_seen=True;self.order.append('ready')
    def drain_lines(self):
        if self.failure=='timeout':return []
        if self.step==0:return [json.dumps(event('authorize',trace=A))]
        if self.step==1:
            stages=['callback_state','token','id_token','userinfo','identity_mapping','local_session','callback_result']
            out=[]
            for name in stages:
                if self.failure==name:
                    out.append(json.dumps(event(name,False,category='rejected')));return out
                e=event(name);e['cookie']=POISON;out.append(json.dumps(e))
                if name=='token' and self.failure!='missing_http':out.append(json.dumps(token_http(2 if self.failure=='retry' else 1)))
            self.session={'opaque':SESSION};return out
        return []
    def session_candidate(self,b):return self.session
    def new_session_candidates(self,b):return [self.session] if self.session else []
    def candidate_belongs_to_run(self,s,m,b,case):return self.failure!='unattributed_session' and m.bound is not None and m.bound.consumed and self.session==s
    def business_probes(self,b,s,case,run_id,callback_trace):
        keys=COMMON|(EXISTING if case=='existing' else JIT);probes={k:True for k in keys}
        if self.browser_source:probes.update(self.browser_source())
        if self.failure=='portal_missing':return None
        if self.failure=='permission':probes['knowledge_access_correct']=False
        if self.failure=='duplicate_jit':probes['one_new_user']=False
        if self.failure=='refresh':probes['no_refresh']=False
        return (OTHER if self.failure=='observer_trace' else C),probes
    def operator_action(self,run_id):return 'authentication_complete' if self.step>=1 else 'password_page_observed'
    def save_safe(self,name,data):
        if self.failure=='evidence_disk' and name=='before-restore':raise OSError(POISON)
        self.order.append('save_'+name);self.saved.append(json.loads(json.dumps(data)))
    def restore_original_app_only(self):
        self.order.append('restore');self.enabled=False;self.flags=dict(CLOSED)
        if self.failure=='restore':raise RuntimeError(POISON)
    def revoke_exact_session_and_access(self,s):self.order.append('revoke');self.revoked.append(s['opaque'])
    def revocation_verified(self,s):return s['opaque'] in self.revoked
    def invalidate_exact_unused(self,tx):self.order.append('invalidate_exact');return 'EXACT_UNUSED_TRANSACTION_INVALIDATED'
    def permissions_preserved(self,b,case):return self.failure!='role_change'
    def close_stream(self):self.order.append('close');self.closed=True


class Calibration(unittest.TestCase):
    def test_company_compose_has_no_legacy_pin_or_old_project(self):
        p=DockerPilotPort.__new__(DockerPilotPort);p.override=RELEASE/'private/test-only-override.json'
        self.assertEqual(PROJECT,'knowhub-company-test')
        self.assertEqual(p.compose(),['docker','--host','unix:///var/run/docker.sock','compose','-p',PROJECT,'--project-directory',str(ROOT),'-f',str(ROOT/'compose.json')])
        self.assertEqual(p.compose(True),p.compose()+['-f',str(p.override)])
        self.assertNotIn('knowhub-oa-sso-pilot',' '.join(p.compose(True)))

    def test_real_runtime_rejects_synthetic_approval_before_files_or_enable(self):
        env={'SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':'false','SOURCEID_BASIC_ENCODING':'oauth_form','SOURCEID_LOCAL_REFRESH_ENABLE':'false','SOURCEID_ACCOUNT_SCOPE':'enterprise_all','SOURCEID_ADMISSION_MODE':'oidc_sso_approved','SOURCEID_SESSION_MODE':'local_approved','AUTO_MIGRATE':'false','SOURCEID_JIT_ENABLE':'false'}
        inputs={'existing_user_id':A,'approved_environment':env,'business_approval_ref':'synthetic-never-execute','scope_evidence_ref':'approved-company-contract','ownership_batch_ref':'approved-one-account'}
        host=json.dumps([{'addr_info':[{'local':'172.30.232.42'}]}]).encode()
        with patch('business_monitor_pilot.command',return_value=host):
            with self.assertRaisesRegex(RuntimeError,'real_approval_required'):DockerPilotPort(inputs,'existing')
            inputs['business_approval_ref']='approved-business-window';env['SOURCEID_LOCAL_POLICY_REF']='synthetic-not-approved'
            with self.assertRaisesRegex(RuntimeError,'real_policy_required'):DockerPilotPort(inputs,'existing')

    def test_company_inventory_rejects_old_project_frontend_ports_and_entry(self):
        p=DockerPilotPort.__new__(DockerPilotPort);p.override=RELEASE/'private/unused.json'
        p.query=lambda q:json.dumps({'version':3029,'dirty':False})
        p.flags=lambda:dict(CLOSED)
        p.check_public_routes=lambda:None
        labels={'com.docker.compose.project':PROJECT,'com.docker.compose.project.working_dir':str(ROOT),'com.docker.compose.project.config_files':str(ROOT/'compose.json')}
        data={(PROJECT+'-app-1','.Config.Labels'):labels,(PROJECT+'-app-1','.Image'):IMAGE,(PROJECT+'-app-1','.HostConfig.PortBindings'):{},(PROJECT+'-postgres-1','.HostConfig.PortBindings'):{},(PROJECT+'-redis-1','.HostConfig.PortBindings'):{},(PROJECT+'-frontend-1','.HostConfig.PortBindings'):{'80/tcp':[{'HostIp':'127.0.0.1','HostPort':'18091'}]},(PROJECT+'-frontend-1','.Image'):FRONT_IMAGE,(ENTRY,'.Config.Labels'):{'com.docker.compose.project':'knowhub-company-entry'},(ENTRY,'.State.Running'):True,(ENTRY,'.HostConfig.PortBindings'):{'80/tcp':[{'HostIp':'0.0.0.0','HostPort':'80'}],'8080/tcp':[{'HostIp':'0.0.0.0','HostPort':'8080'}]},(PROJECT+'-app-1','.HostConfig.NetworkMode'):'container:synthetic-new-namespace',(PROJECT+'-sid-network-1','.Id'):'synthetic-new-namespace',(PROJECT+'-app-1','.State.Health.Status'):'healthy'}
        with patch('business_monitor_pilot.inspect',side_effect=lambda n,f:data[n,f]),patch('business_monitor_pilot.command',return_value=REVISION.encode()):
            self.assertEqual(p.inventory()['project'],PROJECT)
            for key,value in [((PROJECT+'-frontend-1','.Image'),'old-frontend'),((ENTRY,'.State.Running'),False),((PROJECT+'-frontend-1','.HostConfig.PortBindings'),{'80/tcp':[{'HostIp':'127.0.0.1','HostPort':'18090'}]}),((PROJECT+'-app-1','.HostConfig.NetworkMode'),'container:old-pilot-namespace')]:
                old=data[key];data[key]=value
                with self.assertRaises(RuntimeError):p.inventory()
                data[key]=old

    def test_company_public_routes_use_only_no_credential_paths(self):
        class Response:
            def __init__(self,url):
                self.code=302 if url.endswith('/callback') else 200
                self.headers={'X-KnowHub-Environment':'company-test-f40-db3029','Location':'/#oidc_error=invalid_state'}
            def __enter__(self):return self
            def __exit__(self,*args):pass
        requests=[]
        class Opener:
            def open(self,request,timeout):requests.append(request.full_url);return Response(request.full_url)
        with patch('urllib.request.build_opener',return_value=Opener()):DockerPilotPort.__new__(DockerPilotPort).check_public_routes()
        self.assertEqual(len(requests),6)
        self.assertTrue(all('?' not in x and '/authorize' not in x and '/token' not in x for x in requests))
        self.assertTrue(any(':80/' in x for x in requests));self.assertTrue(any(':8080/' in x for x in requests))

    def test_single_account_dryrun_no_evidence_and_no_apply(self):
        import importlib.util,datetime
        path=Path(__file__).with_name('company-first-ownership-dryrun.py')
        spec=importlib.util.spec_from_file_location('single_dryrun',path);module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        with patch.object(module.subprocess,'run',side_effect=AssertionError('must be inert')),contextlib.redirect_stdout(io.StringIO()) as output:module.main([])
        self.assertEqual(json.loads(output.getvalue())['result'],'NOT_EXECUTED')
        now=datetime.datetime.now(datetime.timezone.utc)
        row={'issuer':'https://sid.ruijie.com.cn','employee_no':'fictional','email':'fictional@example.test','user_id':A,'ownership_ref':'approved-record-ref','source_ref':'approved-source-ref','policy_ref':'approved-policy-ref','status':'active','revision':1,'verified_at':(now-datetime.timedelta(minutes=1)).isoformat(),'valid_until':(now+datetime.timedelta(minutes=20)).isoformat()}
        module.validate(row,{'existing_user_id':A},now)
        for key,value in [('employee_no',''),('user_id',OTHER),('ownership_ref','synthetic-not-real'),('policy_ref',''),('valid_until',(now-datetime.timedelta(seconds=1)).isoformat())]:
            with self.assertRaises(ValueError):module.validate(dict(row,**{key:value}),{'existing_user_id':A},now)

    def test_concrete_jit_projection_uses_f40_tenantless_zero(self):
        p=DockerPilotPort.__new__(DockerPilotPort);p.observer_dir=Path('synthetic-observer')
        before={'users':[],'jit':[]}
        session={'id':A,'user_id':C}
        p.new_session_candidates=lambda b:[session]
        p.permissions_preserved=lambda b,c:True
        p.query=lambda q:json.dumps({'local':True,'bounded':True})
        for tenant,expected in ((0,True),(1,False),(None,False)):
            p.snapshot=lambda:{'users':[{'id':C,'tenant_id':tenant,'is_system_admin':False,'can_access_all_tenants':False}],'members':[],'jit':[{'id':A,'user_id':C}]}
            observed={'run_id':A,'callback_trace':C,'checks':dict.fromkeys(('auth_me_200','portal_loaded','no_refresh','knowledge_access_correct','logout_revoked','session_user_matches','access_session_matches'),True)}
            with patch('business_monitor_pilot.atomic'),patch('business_monitor_pilot.protected',return_value=observed),patch.object(Path,'exists',return_value=True):
                trace,checks=p.business_probes(before,session,'jit',A,C)
            self.assertEqual(trace,C)
            self.assertEqual(checks['no_admin_flags'],expected)

    def test_existing_all_roles_same_adapter_and_no_extra_authorize(self):
        for role in ('viewer','contributor','owner','tenant_admin','org_admin','platform_admin','system_admin','cross_space_admin','multi_space'):
            with self.subTest(role=role):
                p=MockPort(role=role);r=run(p,'existing',True,True)
                self.assertEqual(r['result'],'PASS');self.assertEqual(p.revoked,[SESSION]);self.assertEqual(p.flags,CLOSED)
                self.assertLess(p.order.index('save_before-restore'),p.order.index('restore'))
                self.assertLess(p.order.index('restore'),p.order.index('revoke'));self.assertTrue(p.closed)
                self.assertEqual(sum(e['stage']=='authorize' for e in r['events']),1)

    def test_jit_zero_permissions_and_single_user(self):
        p=MockPort('jit');r=run(p,'jit',True,True)
        self.assertEqual(r['result'],'PASS');self.assertTrue(all(r['business_probes'].values()))
        self.assertFalse(r['prompt_login_required']);self.assertFalse(r['auth_time_required'])

    def test_each_protocol_failure_stops_no_retry_and_restores(self):
        for failure in ('callback_state','token','id_token','userinfo','identity_mapping','local_session','callback_result'):
            with self.subTest(stage=failure):
                p=MockPort(failure=failure);r=run(p,'existing',True,True)
                self.assertEqual(r['result'],'FAIL');self.assertEqual(p.flags,CLOSED)
                self.assertEqual(sum(e['stage']=='authorize' for e in r['events']),1)

    def test_waiting_password_page_does_not_finish(self):
        m=BusinessMonitor('existing',100);m.transactions([Transaction(PRIVATE,100,700,False,False)],101)
        m.operator('password_page_observed');self.assertFalse(m.finished)
        m.tick(579);self.assertFalse(m.finished)

    def test_timeout_bounded_by_transaction_lifetime(self):
        p=MockPort(failure='timeout');r=run(p,'existing',True,True)
        self.assertEqual(r['result'],'INCOMPLETE');self.assertEqual(r['reason'],'INCOMPLETE_TIMEOUT')
        self.assertLessEqual(p.clock,110);self.assertEqual(p.flags,CLOSED)
        self.assertEqual(r['stages']['token'],'NOT_EXECUTED')
        self.assertIn('invalidate_exact',p.order)

    def test_second_authorize_or_unknown_callback_is_ambiguous(self):
        p=MockPort(failure='second_authorize');r=run(p,'existing',True,True)
        self.assertEqual(r['reason'],'CORRELATION_AMBIGUOUS');self.assertEqual(p.revoked,[])
        m=BusinessMonitor('existing',100);m.line(json.dumps(event('callback_state')))
        self.assertEqual(m.reason,'CORRELATION_AMBIGUOUS')

    def test_prompt_true_not_accepted_as_ordinary_sso(self):
        m=BusinessMonitor('existing',100);m.transactions([Transaction(PRIVATE,100,700,False,True)],100)
        self.assertEqual(m.reason,'CORRELATION_AMBIGUOUS')

    def test_consumed_first_observation_not_guessed(self):
        m=BusinessMonitor('jit',100);m.transactions([Transaction(PRIVATE,100,700,True,False)],101)
        self.assertEqual(m.reason,'CORRELATION_AMBIGUOUS')

    def test_bus_failure_cases_restore_and_revoke(self):
        for case,failure in [('existing','permission'),('jit','duplicate_jit'),('jit','refresh'),('existing','observer_trace')]:
            with self.subTest(failure=failure):
                p=MockPort(case,failure);r=run(p,case,True,True)
                self.assertEqual(r['result'],'FAIL');self.assertEqual(p.flags,CLOSED)
                if failure!='observer_trace':self.assertEqual(p.revoked,[SESSION])

    def test_no_portal_evidence_cannot_pass(self):
        p=MockPort(failure='portal_missing');r=run(p,'existing',True,True)
        self.assertEqual(r['result'],'INCOMPLETE');self.assertEqual(r['stages']['portal'],'NOT_CAPTURED')
        self.assertEqual(p.revoked,[SESSION])

    def test_missing_http_or_retry_cannot_pass(self):
        p=MockPort(failure='missing_http');r=run(p,'existing',True,True)
        self.assertEqual(r['result'],'INCOMPLETE');self.assertEqual(r['stages']['token_http'],'NOT_CAPTURED')
        p=MockPort(failure='retry');r=run(p,'existing',True,True)
        self.assertEqual(r['reason'],'UNEXPECTED_TOKEN_RETRY');self.assertEqual(r['result'],'FAIL')

    def test_partial_enable_restores_and_stream_closes(self):
        p=MockPort(failure='partial_enable');r=run(p,'existing',True,True)
        self.assertEqual(r['result'],'FAIL');self.assertEqual(p.flags,CLOSED);self.assertTrue(p.closed)

    def test_db_failure_at_restore_cannot_prevent_closing_gates(self):
        p=MockPort();a=PilotBusinessAdapter(p,'existing',True,True);m=a.prepare(100);m.end('CANCELLED_INCOMPLETE')
        p.transactions=lambda:(_ for _ in ()).throw(RuntimeError(POISON))
        r=finish(m,a)
        self.assertEqual(p.flags,CLOSED);self.assertIn('restore',p.order);self.assertEqual(r['result'],'FAIL')

    def test_evidence_failure_does_not_skip_restore_or_revocation(self):
        p=MockPort(failure='evidence_disk');r=run(p,'existing',True,True)
        self.assertEqual(r['result'],'FAIL');self.assertIn('restore',p.order);self.assertTrue(p.closed)

    def test_restore_failure_never_report_pass_and_revocation_still_attempted(self):
        p=MockPort(failure='restore');r=run(p,'existing',True,True)
        self.assertEqual(r['result'],'FAIL');self.assertEqual(r['recovery'],'REQUIRES_REVIEW');self.assertEqual(p.revoked,[SESSION])

    def test_permissions_cannot_change_on_restore(self):
        r=run(MockPort(failure='role_change'),'existing',True,True)
        self.assertEqual(r['result'],'FAIL');self.assertEqual(r['restore_verification'],'FAIL')

    def test_no_raw_or_handle_leak(self):
        p=MockPort();r=run(p,'existing',True,True);data=json.dumps([r,p.saved])
        for forbidden in (POISON,PRIVATE,SESSION):self.assertNotIn(forbidden,data)

    def test_unknown_fields_and_oversized_lines_reject(self):
        m=BusinessMonitor('jit',100);m.stage({'token':POISON});self.assertEqual(m.reason,'OBSERVABILITY_INCOMPLETE')
        m=BusinessMonitor('jit',100);m.line('x'*65537);self.assertEqual(m.reason,'OBSERVABILITY_INCOMPLETE')

    def test_default_cli_inert_without_docker_or_file_access(self):
        with patch('business_monitor_pilot.command',side_effect=AssertionError('must not call Docker')),contextlib.redirect_stdout(io.StringIO()) as output:
            self.assertEqual(main([]),0)
        self.assertEqual(json.loads(output.getvalue())['result'],'NOT_EXECUTED')

    def test_runtime_refuses_wrong_host_before_reading_inputs(self):
        with patch('business_monitor_pilot.command',return_value=b'[]') as call:
            with self.assertRaisesRegex(RuntimeError,'fixed_packaging_host_required'):DockerPilotPort({},'existing')
            self.assertEqual(call.call_args.args[0],['ip','-j','-4','address','show'])

    def test_explicit_authorization_and_window_both_required(self):
        for a,b in ((False,False),(True,False),(False,True)):
            p=MockPort();r=run(p,'existing',a,b);self.assertEqual(r['result'],'FAIL');self.assertNotIn('enable',p.order)

    def test_fixed_target_version_schema_project_image(self):
        for field,value in [('project','another-project'),('schema',3028),('revision','unknown'),('image','sha256:other'),('dirty',True)]:
            p=MockPort();original=p.inventory
            p.inventory=lambda f=field,v=value:dict(original(),**{f:v})
            r=run(p,'existing',True,True);self.assertEqual(r['result'],'FAIL');self.assertNotIn('enable',p.order)

    def test_out_of_order_callback_stages_not_pass(self):
        m=BusinessMonitor('existing',100);t=Transaction(PRIVATE,100,700,False,False);m.transactions([t],100)
        m.line(json.dumps(event('authorize',trace=A)));m.transactions([Transaction(PRIVATE,100,700,True,False)],101)
        m.line(json.dumps(event('token')));self.assertEqual(m.reason,'OBSERVABILITY_INCOMPLETE')

    def test_real_mock_http_browser_channel_not_just_http200(self):
        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self,*a):pass
            def do_GET(self):
                status=403 if self.path=='/denied-kb' else 200
                self.send_response(status);self.send_header('Content-Type','application/json');self.end_headers()
                self.wfile.write(json.dumps({'authenticated':self.path=='/auth/me','portal':self.path=='/portal','permission':'allowed' if self.path=='/allowed-kb' else 'denied'}).encode())
        server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler);thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            def observe():
                def get(path):
                    try:r=urllib.request.build_opener(urllib.request.ProxyHandler({})).open('http://127.0.0.1:'+str(server.server_port)+path,timeout=3)
                    except urllib.error.HTTPError as e:r=e
                    with r:return r.status,json.load(r)
                auth,a=get('/auth/me');portal,p=get('/portal');allow,k=get('/allowed-kb');deny,_=get('/denied-kb')
                return {'auth_me_200':auth==200 and a['authenticated'],'portal_loaded':portal==200 and p['portal'],'knowledge_access_correct':allow==200 and k['permission']=='allowed' and deny==403}
            p=MockPort();p.browser_source=observe;r=run(p,'existing',True,True)
            self.assertEqual(r['result'],'PASS')
        finally:server.shutdown();server.server_close();thread.join()


if __name__=='__main__':unittest.main()
