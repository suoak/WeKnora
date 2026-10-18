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

from business_monitor import BusinessMonitor, COMMON, EXISTING, JIT, IMAGE, PROJECT, REVISION, SCHEMA, Transaction, finish
from business_monitor_adapter import CLOSED, run, PilotBusinessAdapter
from business_monitor_pilot import main, DockerPilotPort, ROOT, RELEASE, ENTRY, FRONT_IMAGE, COMPOSE_FILES, ENVIRONMENT_HEADER, approved_scope
from company_business_observer import ServerBusinessObserver
from company_gateway_observer import GatewayObservation,configuration

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
        return {'project':PROJECT,'image':IMAGE,'revision':REVISION,'schema':SCHEMA,'dirty':False,'healthy':True,'flags':dict(self.flags)}
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

    def test_gateway_uuid_path_map_has_explicit_bucket_size(self):
        original='http { access_log off; allow 192.0.2.10; }'
        value=configuration(original,'192.0.2.10',[A],[C],None)
        self.assertIn('map_hash_bucket_size 128;',value)
        existing=configuration('http { map_hash_bucket_size 256; access_log off; allow 192.0.2.10; }','192.0.2.10',[A],[C],None)
        self.assertEqual(existing.count('map_hash_bucket_size'),1)

    def test_preexisting_oidc_entry_is_preserved_with_business_closed(self):
        port=MockPort();port.closed_baseline=dict(CLOSED,OIDC_AUTH_ENABLE='true');port.flags=dict(port.closed_baseline)
        original=port.restore_original_app_only
        def restore():
            original();port.flags['OIDC_AUTH_ENABLE']='true'
        port.restore_original_app_only=restore
        result=run(port,'existing',True,True)
        self.assertEqual(result['result'],'PASS')
        self.assertEqual(port.flags,port.closed_baseline)

    def test_preexisting_entry_compose_layer_survives_restore(self):
        port=DockerPilotPort.__new__(DockerPilotPort);port.base_compose=COMPOSE_FILES+['/protected/approved-entry-only.json'];port.override=Path('/protected/one-business-window.json')
        self.assertEqual(port.compose(False)[-2:],['-f','/protected/approved-entry-only.json'])
        self.assertEqual(port.compose(True)[-4:],['-f','/protected/approved-entry-only.json','-f',str(port.override)])

    def test_positive_scope_requires_explicit_real_approval(self):
        self.assertEqual(approved_scope({}, {}, 'existing'),'full')
        for value in ({'acceptance_scope':'A_positive'},{'acceptance_scope':'A_positive','positive_scope_approval_ref':'synthetic-not-approved'},{'acceptance_scope':'unknown'}):
            with self.assertRaises(RuntimeError):approved_scope(value,{},'existing')
        value={'acceptance_scope':'A_positive','positive_scope_approval_ref':'approved-positive-only-window'}
        self.assertEqual(approved_scope(value,{},'existing'),'A_positive')
        with self.assertRaises(RuntimeError):approved_scope(value,{},'jit')

    def test_positive_scope_never_reports_negative_pass(self):
        observer=ServerBusinessObserver('192.0.2.10',[A],[C],None,False)
        for path,method in (('/api/v1/auth/me','GET'),('/api/v1/knowledge-bases/'+A,'GET'),('/api/v1/knowledge/'+C,'GET'),('/api/v1/auth/logout','POST')):observer.observe(self.access_line(path,method=method),True)
        observer.confirm_operator({'action':'authentication_complete','portal_confirmed':True})
        self.assertTrue(observer.complete());self.assertNotIn('denied_kb',observer.seen)
        p=MockPort();p.acceptance_scope='A_positive';p.link_policy_status='PASS'
        receipt=run(p,'existing',True,True)
        self.assertEqual(receipt['result'],'PASS');self.assertEqual(receipt['acceptance_scope'],'A_positive');self.assertEqual(receipt['cross_space_negative'],'NOT_EXECUTED');self.assertEqual(receipt['link_policy_verification'],'PASS')

    def test_new_policy_requires_company_approval_not_ownership_batch(self):
        env={'SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':'false','SOURCEID_BASIC_ENCODING':'oauth_form','SOURCEID_LOCAL_REFRESH_ENABLE':'false','SOURCEID_ACCOUNT_SCOPE':'enterprise_all','SOURCEID_ADMISSION_MODE':'oidc_sso_approved','SOURCEID_SESSION_MODE':'local_approved','AUTO_MIGRATE':'false','SOURCEID_JIT_ENABLE':'false','SOURCEID_ACCOUNT_LINK_MODE':'verified_email_policy','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED':'true','SOURCEID_VERIFIED_EMAIL_POLICY_REF':'approved-company-policy'}
        inputs={'existing_user_id':A,'approved_environment':env,'business_approval_ref':'approved-window','scope_evidence_ref':'approved-company-scope','company_policy_ref':'wrong-policy'}
        host=json.dumps([{'addr_info':[{'local':'172.30.232.42'}]}]).encode()
        with patch('business_monitor_pilot.command',return_value=host):
            with self.assertRaisesRegex(RuntimeError,'company_email_policy_approval_required'):DockerPilotPort(inputs,'existing')
            inputs['company_policy_ref']='approved-company-policy';inputs['acceptance_scope']='A_positive'
            # Proves there was no per-person ownership_batch_ref requirement,
            # but the independent positive-only scope cannot be assumed.
            with self.assertRaisesRegex(RuntimeError,'explicit_positive_scope_approval_required'):DockerPilotPort(inputs,'existing')

    def test_verified_policy_metadata_and_no_fabricated_ownership_required(self):
        p=DockerPilotPort.__new__(DockerPilotPort);p.user=A
        p.env={'SOURCEID_ACCOUNT_LINK_MODE':'verified_email_policy','SOURCEID_VERIFIED_EMAIL_POLICY_REF':'synthetic-company-policy','SOURCEID_AUTO_BINDING_POLICY_REF':'synthetic-auto-policy'}
        class Observer:
            def complete(self):return True
            def checks(self,db):return dict.fromkeys(('auth_me_200','portal_loaded','no_refresh','knowledge_access_correct','logout_revoked','session_user_matches','access_session_matches'),True)
        p.server_observer=Observer();before={'users':[{'id':A}],'members':[],'acl':[],'ownership':[]}
        after=copy.deepcopy(before);p.snapshot=lambda:after;p.permissions_preserved=lambda b,c:True
        p.new_session_candidates=lambda b:[{'id':C,'user_id':A}]
        marker={'link_mode':'verified_email_policy','policy_ref':'synthetic-company-policy','binding_policy_ref':'synthetic-auto-policy'}
        def query(sql):
            if 'source_id_local_link_policies' in sql:return json.dumps([marker])
            if "'local',COUNT" in sql:return json.dumps({'local':True,'bounded':True})
            return json.dumps(dict.fromkeys(('no_refresh','session_user_matches','access_session_matches','logout_revoked'),True))
        p.query=query
        _,checks=p.business_probes(before,{'id':C,'user_id':A},'existing',A,C)
        self.assertTrue(all(checks.values()));self.assertEqual(p.link_policy_status,'PASS')
        marker['policy_ref']='different-policy'
        with self.assertRaisesRegex(RuntimeError,'verified_email_policy_metadata_mismatch'):p.business_probes(before,{'id':C,'user_id':A},'existing',A,C)
        marker['policy_ref']='synthetic-company-policy';after['ownership']=[{'id':OTHER}]
        with self.assertRaisesRegex(RuntimeError,'verified_email_policy_metadata_mismatch'):p.business_probes(before,{'id':C,'user_id':A},'existing',A,C)
    def test_gateway_observer_accepts_only_closed_safe_metadata(self):
        observer=ServerBusinessObserver('192.0.2.10',[A],[C],OTHER)
        value={'event':'company_business_http','kind':'auth_me','method':'GET','status':200,'source_approved':True,'request_id':'a'*32}
        for row in (dict(value,cookie=POISON),dict(value,source_approved=False),dict(value,request_id=POISON)):
            observer.observe(json.dumps(row),True)
        self.assertFalse(observer.seen)
        observer.observe(json.dumps(value),True);self.assertEqual(observer.seen,{'auth_me'})
        observer.observe(json.dumps(dict(value,kind='denied_kb',status=200,request_id='b'*32)),True);self.assertTrue(observer.failed)

    def test_gateway_configuration_never_logs_headers_queries_body_or_identity(self):
        original='http { access_log off; server { allow 192.0.2.10; deny all; } }'
        text=configuration(original,'192.0.2.10',[A],[C],OTHER)
        for variable in ('$request_uri','$http_cookie','$http_authorization','$request_body','$upstream_http_set_cookie','$remote_addr"'):
            self.assertNotIn(variable,text)
        self.assertIn('escape=json',text);self.assertIn('map $uri',text);self.assertIn('access_log /dev/stdout kh_business if=$kh_emit;',text)
        with self.assertRaises(ValueError):configuration(original,'192.0.2.11',[A],[C],OTHER)

    def test_gateway_metadata_config_restores_exact_bytes(self):
        class File:
            def __init__(self):self.data=b'http { access_log off; server { allow 192.0.2.10; } }'
            def read_bytes(self):return self.data
            def write_bytes(self,v):self.data=v
            def write_text(self,v):self.data=v.encode()
        file=File();original=file.data;calls=[]
        with patch('company_gateway_observer.CONFIG',file):
            control=GatewayObservation(lambda args:calls.append(args),lambda n,f:{'Type':'none'},'192.0.2.10',[A],[C],OTHER)
            control.enable();self.assertNotEqual(file.data,original)
            control.restore();self.assertEqual(file.data,original);self.assertIsNone(control.original)
            bad=GatewayObservation(lambda args:None,lambda n,f:{'Type':'json-file'},'192.0.2.10',[A],[C],OTHER)
            with self.assertRaisesRegex(RuntimeError,'gateway_raw_persistence_not_allowed'):bad.enable()

    def test_gateway_restore_runs_even_if_app_restore_fails(self):
        p=DockerPilotPort.__new__(DockerPilotPort)
        p.recreate=lambda _:(_ for _ in ()).throw(RuntimeError('synthetic restore failure'))
        class Gateway:
            restored=False
            def restore(self):self.restored=True
        p.gateway_observation=Gateway()
        with self.assertRaises(RuntimeError):p.restore_original_app_only()
        self.assertTrue(p.gateway_observation.restored)

    def test_server_metadata_ready_does_not_inject_unowned_callback(self):
        p=DockerPilotPort.__new__(DockerPilotPort);p.server_observer=object();p.dir=Path('synthetic-unused')
        p.check_public_routes=lambda:(_ for _ in ()).throw(AssertionError('must not inject callback after attach'))
        with patch('business_monitor_pilot.atomic') as save:p.ready(A)
        self.assertEqual(save.call_args.args[1]['phase'],'READY')

    @staticmethod
    def access_line(path,status=200,method='GET',source='192.0.2.10'):
        import uuid
        return 'INFO[2026-10-17] ['+str(uuid.uuid4())+' client_ip='+source+' latency=1ms method='+method+' path='+path+' response_body='+POISON+' size=1 status_code='+str(status)+'] logger.go | '

    def test_server_observer_metadata_and_user_portal_are_separate(self):
        observer=ServerBusinessObserver('192.0.2.10',[A],[C],OTHER)
        paths=[('/api/v1/auth/me',200,'GET'),('/api/v1/knowledge-bases/'+A,200,'GET'),('/api/v1/knowledge/'+C+'/preview',200,'GET'),('/api/v1/knowledge-bases/'+OTHER,403,'GET'),('/api/v1/auth/logout',200,'POST')]
        for path,status,method in paths:observer.observe(self.access_line(path,status,method),True)
        self.assertFalse(observer.complete())
        observer.confirm_operator({'action':'authentication_complete','portal_confirmed':True})
        self.assertTrue(observer.complete())
        db=dict.fromkeys(('no_refresh','logout_revoked','session_user_matches','access_session_matches'),True)
        self.assertTrue(all(observer.checks(db).values()))
        self.assertNotIn(POISON,json.dumps(observer.checks(db)))

    def test_server_observer_ignores_wrong_source_pre_callback_and_unknown_logs(self):
        observer=ServerBusinessObserver('192.0.2.10',[A],[C],OTHER)
        observer.observe(self.access_line('/api/v1/auth/me'),False)
        observer.observe(self.access_line('/api/v1/auth/me',source='192.0.2.99'),True)
        observer.observe('path=/api/v1/auth/me status_code=200 '+POISON,True)
        self.assertFalse(observer.seen);self.assertFalse(observer.failed)
        observer.observe(self.access_line('/api/v1/knowledge-bases/'+OTHER,200),True)
        self.assertTrue(observer.failed)

    def test_server_observer_resource_absence_or_refresh_cannot_pass(self):
        observer=ServerBusinessObserver('192.0.2.10',[A],[C],None)
        observer.confirm_operator({'action':'authentication_complete','portal_confirmed':True})
        for path in ('/api/v1/auth/me','/api/v1/knowledge-bases/'+A,'/api/v1/knowledge/'+C):observer.observe(self.access_line(path),True)
        self.assertFalse(observer.complete())
        db={'no_refresh':False,'logout_revoked':True,'session_user_matches':True,'access_session_matches':True}
        self.assertFalse(observer.checks(db)['no_refresh'])
        with self.assertRaises(ValueError):ServerBusinessObserver('192.0.2.10',[A],[C],A)

    def test_server_observer_full_adapter_run_saves_restores_and_revokes(self):
        class ServerPort(MockPort):
            def __init__(self):
                super().__init__();self.server_observer=ServerBusinessObserver('192.0.2.10',[A],[C],OTHER)
            def drain_lines(self):
                lines=super().drain_lines()
                if self.step==2:
                    for path,status,method in [('/api/v1/auth/me',200,'GET'),('/api/v1/knowledge-bases/'+A,200,'GET'),('/api/v1/knowledge/'+C,200,'GET'),('/api/v1/knowledge-bases/'+OTHER,403,'GET'),('/api/v1/auth/logout',200,'POST')]:lines.append(Calibration.access_line(path,status,method))
                return lines
            def observe_business_lines(self,lines,m):
                for line in lines:self.server_observer.observe(line,m.stages['callback_result']=='PASS')
            def business_probes(self,before,private,case,run_id,callback_trace):
                if not self.server_observer.complete():return None
                trace,values=super().business_probes(before,private,case,run_id,callback_trace)
                values.update(self.server_observer.checks(dict.fromkeys(('no_refresh','logout_revoked','session_user_matches','access_session_matches'),True)))
                return trace,values
            def operator_action(self,run_id):
                if self.step>=2:self.server_observer.confirm_operator({'action':'authentication_complete','portal_confirmed':True});return 'authentication_complete'
        p=ServerPort();r=run(p,'existing',True,True)
        self.assertEqual(r['result'],'PASS');self.assertEqual(r['observation_mode'],'SERVER_METADATA_AND_USER_PORTAL_CONFIRMATION');self.assertEqual(p.flags,CLOSED);self.assertEqual(p.revoked,[SESSION])
        self.assertLess(p.order.index('save_before-restore'),p.order.index('restore'))

    def test_company_compose_has_no_legacy_pin_or_old_project(self):
        p=DockerPilotPort.__new__(DockerPilotPort);p.override=RELEASE/'private/test-only-override.json'
        self.assertEqual(PROJECT,'knowhub-company-test')
        self.assertEqual(p.compose(),['docker','--host','unix:///var/run/docker.sock','compose','-p',PROJECT,'--project-directory',str(ROOT),'-f',COMPOSE_FILES[0],'-f',COMPOSE_FILES[1]])
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
        p.query=lambda q:json.dumps({'version':SCHEMA,'dirty':False})
        p.flags=lambda:dict(CLOSED)
        p.check_public_routes=lambda:None
        labels={'com.docker.compose.project':PROJECT,'com.docker.compose.project.working_dir':str(ROOT),'com.docker.compose.project.config_files':','.join(COMPOSE_FILES)}
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
                self.headers={'X-KnowHub-Environment':ENVIRONMENT_HEADER,'Location':'/#oidc_error=invalid_state'}
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

    def test_login_result_handoff_is_not_an_authorization(self):
        # Real server stores authorization and login-result handoff in the same
        # table. Handoff has empty nonce and must not look like a second login.
        p=object.__new__(DockerPilotPort);p.tx_cache={}
        def query(sql):
            self.assertIn("FROM oidc_transactions WHERE nonce<>''",sql)
            return json.dumps([{'key':PRIVATE,'consumed':False,'expires':700,'started':100,'prompt':False}])
        p.query=query
        rows=p.transactions();self.assertEqual(len(rows),1)
        m=BusinessMonitor('existing',100);m.transactions(rows,100)
        self.assertFalse(m.finished);self.assertEqual(m.bound.handle,PRIVATE)

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
