"""Inert by default. Fixed company-test Docker target; no SID client.

Protected browser observations come from the controlled browser observer, not
Cookie/HAR/token exports. No business PASS without that channel and DB checks.
"""
import argparse
import json
import os
from pathlib import Path
import re
import select
import subprocess
import time
import uuid

from business_monitor import PROJECT, IMAGE, REVISION, SCHEMA, COMMON, EXISTING, JIT
from business_monitor_adapter import CLOSED, run
from protocol_monitor import Transaction
from company_business_observer import ServerBusinessObserver
from company_gateway_observer import GatewayObservation

RELEASE=Path('/home/aqyfzx/ai/knowhub-company-entry-20261017')
ROOT=RELEASE/'green'
APP_PIN=Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51/app-pin.json')
COMPOSE_FILES=[str(ROOT/'compose.json'),str(APP_PIN)]
ENVIRONMENT_HEADER='company-test-verified-email-db3030'
FRONT_IMAGE='sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1'
FRONT_REVISION='832b6652adf541892545a7eac8b68733f846ab95'
ENTRY='knowhub-company-entry-entry-1'
APP=PROJECT+'-app-1';PG=PROJECT+'-postgres-1'
DOCKER=['docker','--host','unix:///var/run/docker.sock']
UUID=re.compile(r'[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}')
FLAGS=set(CLOSED)|{'SOURCEID_ACCOUNT_SCOPE','SOURCEID_ADMISSION_MODE','SOURCEID_DIAGNOSTICS_ENABLE','SOURCEID_JIT_ENABLE'}
ENV_ALLOWED=FLAGS|{'SOURCEID_CLAIMS_SOURCE','SOURCEID_EMPLOYEE_CLAIM','SOURCEID_ACCOUNT_LINK_MODE','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED','SOURCEID_VERIFIED_EMAIL_POLICY_REF','SOURCEID_EMAIL_CONTRACT_CONFIRMED','SOURCEID_SSO_POLICY_CONFIRMED','SOURCEID_ENTERPRISE_POLICY_CONFIRMED','SOURCEID_JIT_POLICY_CONFIRMED','SOURCEID_AUTO_BINDING_POLICY_REF','SOURCEID_LOCAL_POLICY_REF','SOURCEID_ADMISSION_POLICY_REF','SOURCEID_ENTERPRISE_POLICY_REF','SOURCEID_JIT_POLICY_REF','SOURCEID_JIT_EMAIL_DOMAINS','SOURCEID_ENTERPRISE_MAX_AGE','SOURCEID_LOCAL_ACCESS_TTL','SOURCEID_LOCAL_ABSOLUTE_TTL','SOURCEID_LOCAL_IDLE_TTL','SOURCEID_ADMISSION_MAX_AGE'}

def approved_scope(inputs,env,case):
    scope=inputs.get('acceptance_scope','full')
    if scope not in ('full','A_positive'):raise RuntimeError('invalid_acceptance_scope')
    if scope=='A_positive':
        ref=inputs.get('positive_scope_approval_ref','')
        if case!='existing' or not isinstance(ref,str) or not ref.strip() or any(w in ref.lower() for w in ('synthetic','mock','placeholder','todo')):raise RuntimeError('explicit_positive_scope_approval_required')
    return scope


def command(args, data=None, timeout=30):
    p=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
    if p.returncode:raise RuntimeError('controlled_pilot_command_failed')
    return p.stdout


def inspect(name, field):
    return json.loads(command(DOCKER+['inspect','--format','{{json '+field+'}}',name]))


def atomic(path, value):
    raw=json.dumps(value,sort_keys=True).encode()
    if len(raw)>1048576:raise RuntimeError('evidence_budget')
    tmp=path.with_suffix('.tmp')
    with tmp.open('wb') as f:f.write(raw);f.flush();os.fsync(f.fileno())
    tmp.chmod(0o600);tmp.replace(path)


def protected(path, maximum=65536):
    path=Path(path)
    if path.is_symlink() or not path.is_file() or path.stat().st_uid!=os.getuid() or path.stat().st_mode & 0o077 or path.stat().st_size>maximum:
        raise RuntimeError('protected_input_required')
    # No duplicate policy/observation fields.
    def unique(pairs):
        d={}
        for k,v in pairs:
            if k in d:raise ValueError('duplicate_field')
            d[k]=v
        return d
    return json.loads(path.read_text(encoding='utf-8'),object_pairs_hook=unique)


class DockerPilotPort:
    def __init__(self, inputs, case):
        interfaces=json.loads(command(['ip','-j','-4','address','show']))
        if not any(a.get('local')=='172.30.232.42' for n in interfaces for a in n.get('addr_info',[])):
            raise RuntimeError('fixed_packaging_host_required')
        self.case=case;self.inputs=inputs;self.process=None;self.buffer=b'';self.tx_cache={};self.run_id=None
        self.base_compose=list(COMPOSE_FILES);self.closed_baseline=dict(CLOSED)
        if inputs.get('existing_entry_override'):
            entry=Path(inputs['existing_entry_override']).resolve()
            entry.relative_to(Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51/private').resolve())
            if entry.name!='approved-entry-only.json' or protected(entry)!={'services':{'app':{'environment':{'OIDC_AUTH_ENABLE':'true'}}}}:raise RuntimeError('invalid_existing_entry_override')
            self.base_compose.append(str(entry));self.closed_baseline['OIDC_AUTH_ENABLE']='true'
        self.user=inputs.get('existing_user_id')
        if case=='existing' and (not isinstance(self.user,str) or not UUID.fullmatch(self.user)):
            raise RuntimeError('protected_existing_account_required')
        if case=='jit' and self.user is not None:raise RuntimeError('jit_cannot_target_legacy_user')
        self.env=inputs['approved_environment']
        if set(self.env)-ENV_ALLOWED or any(not isinstance(v,str) or '\n' in v or '\r' in v for v in self.env.values()):
            raise RuntimeError('override_not_whitelisted')
        fixed={'SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':'false','SOURCEID_BASIC_ENCODING':'oauth_form',
               'SOURCEID_LOCAL_REFRESH_ENABLE':'false','SOURCEID_ACCOUNT_SCOPE':'enterprise_all',
               'SOURCEID_ADMISSION_MODE':'oidc_sso_approved','SOURCEID_SESSION_MODE':'local_approved',
               'AUTO_MIGRATE':'false','SOURCEID_JIT_ENABLE':'true' if case=='jit' else 'false'}
        if any(self.env.get(k)!=v for k,v in fixed.items()) or self.env.get('SOURCEID_ACCOUNT_LINK_MODE','trusted_ownership') not in ('trusted_ownership','verified_email_policy'):raise RuntimeError('approved_mode_mismatch')
        evidence_keys=['business_approval_ref','scope_evidence_ref']
        if getattr(self,'env',{}).get('SOURCEID_ACCOUNT_LINK_MODE')=='verified_email_policy':
            if self.env.get('SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED')!='true' or inputs.get('company_policy_ref')!=self.env.get('SOURCEID_VERIFIED_EMAIL_POLICY_REF'):raise RuntimeError('company_email_policy_approval_required')
            evidence_keys.append('company_policy_ref')
        else:evidence_keys.append('ownership_batch_ref')
        for key in evidence_keys:
            if not isinstance(inputs.get(key),str) or not inputs[key].strip():raise RuntimeError('approval_input_missing')
            if any(word in inputs[key].lower() for word in ('synthetic','mock','placeholder','todo')):raise RuntimeError('real_approval_required')
        for key,value in self.env.items():
            if key.endswith('_POLICY_REF') and (not value.strip() or any(word in value.lower() for word in ('synthetic','mock','placeholder','todo'))):raise RuntimeError('real_policy_required')
        self.acceptance_scope=approved_scope(inputs,self.env,case)
        self.dir=RELEASE/'private'/('business-monitor-'+str(uuid.uuid4()));self.dir.mkdir(mode=0o700)
        self.override=self.dir/'approved-once.json'
        self.observer_dir=Path(inputs['protected_observer_directory']).resolve()
        self.observer_dir.relative_to((RELEASE/'private').resolve())
        if self.observer_dir.is_symlink() or self.observer_dir.stat().st_uid!=os.getuid() or self.observer_dir.stat().st_mode & 0o077:
            raise RuntimeError('protected_observer_required')
        self.server_observer=None;self.gateway_observation=None;self.gateway_process=None
        if inputs.get('observation_mode')=='server_metadata':
            if case!='existing':raise RuntimeError('first_round_existing_only')
            self.server_observer=ServerBusinessObserver(inputs['approved_client_address'],inputs['allowed_kb_ids'],inputs['allowed_document_ids'],inputs.get('denied_kb_id'),self.acceptance_scope!='A_positive')
            if self.server_observer.require_negative and not self.server_observer.denied:raise RuntimeError('existing_negative_resource_not_ready')
            self.validate_observation_resources()
            self.gateway_observation=GatewayObservation(command,inspect,inputs['approved_client_address'],self.server_observer.allowed,self.server_observer.documents,self.server_observer.denied)

    def now(self):return time.time()
    def pause(self):time.sleep(.25)

    def query(self,q,write=False):
        # Never request result, nonce, verifier, tokens, email, sub or RJGH.
        prefix="SET log_statement='none';SET log_min_error_statement='panic';SET log_parameter_max_length_on_error=0;"
        sql=prefix+('BEGIN;' if write else 'BEGIN READ ONLY;')+q+(';COMMIT;' if write else ';ROLLBACK;')
        return command(DOCKER+['exec','-i',PG,'psql','-X','-U','clean_owner','-d','knowhub_oa_sso_pilot','-qAt','-v','ON_ERROR_STOP=1'],sql.encode()).decode().strip()

    def flags(self):
        # Query only reviewed policy keys in the APP process. Never retrieve
        # the entire Docker environment (which includes real credentials).
        script="import os,json,sys;print(json.dumps({k:os.environ[k] for k in json.loads(sys.argv[1]) if k in os.environ}))"
        env=json.loads(command(DOCKER+['exec',APP,'python3','-c',script,json.dumps(sorted(ENV_ALLOWED))]))
        return {k:env.get(k,'trusted_ownership' if k=='SOURCEID_ACCOUNT_LINK_MODE' else ('false' if k in ('SOURCEID_JIT_ENABLE','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED') else 'NOT_SET')) for k in ENV_ALLOWED}

    def inventory(self):
        labels=inspect(APP,'.Config.Labels')
        if labels.get('com.docker.compose.project.working_dir')!=str(ROOT):raise RuntimeError('project_directory_mismatch')
        base=getattr(self,'base_compose',COMPOSE_FILES)
        if labels.get('com.docker.compose.project.config_files','').split(',') not in (base,base+[str(self.override)]):raise RuntimeError('compose_order_mismatch')
        image=inspect(APP,'.Image')
        if inspect(APP,'.HostConfig.PortBindings') or inspect(PG,'.HostConfig.PortBindings') or inspect(PROJECT+'-redis-1','.HostConfig.PortBindings'):
            raise RuntimeError('pilot_private_ports_changed')
        if inspect(PROJECT+'-frontend-1','.HostConfig.PortBindings')!={'80/tcp':[{'HostIp':'127.0.0.1','HostPort':'18091'}]}:
            raise RuntimeError('company_internal_frontend_changed')
        if inspect(PROJECT+'-frontend-1','.Image')!=FRONT_IMAGE:
            raise RuntimeError('company_frontend_image_changed')
        if inspect(ENTRY,'.Config.Labels').get('com.docker.compose.project')!='knowhub-company-entry' or not inspect(ENTRY,'.State.Running'):
            raise RuntimeError('company_public_entry_changed')
        if inspect(ENTRY,'.HostConfig.PortBindings')!={'80/tcp':[{'HostIp':'0.0.0.0','HostPort':'80'}],'8080/tcp':[{'HostIp':'0.0.0.0','HostPort':'8080'}]}:
            raise RuntimeError('company_public_ports_changed')
        self.check_public_routes()
        if inspect(APP,'.HostConfig.NetworkMode')!='container:'+inspect(PROJECT+'-sid-network-1','.Id'):
            raise RuntimeError('pilot_guard_namespace_changed')
        revision=command(DOCKER+['image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',image]).decode().strip()
        schema=json.loads(self.query("SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations"))
        return {'project':labels.get('com.docker.compose.project'),'image':image,'revision':revision,'schema':schema['version'],'dirty':schema['dirty'],'healthy':inspect(APP,'.State.Health.Status')=='healthy','flags':self.flags()}

    def other_services(self):
        values={s:(inspect(PROJECT+'-'+s+'-1','.Id'),inspect(PROJECT+'-'+s+'-1','.Image')) for s in ('frontend','postgres','redis','docreader','sid-network')}
        values['public_entry']=(inspect(ENTRY,'.Id'),inspect(ENTRY,'.Image'))
        return values

    def check_public_routes(self):
        # Public callbacks carry no code/state; never request authorize URL.
        import urllib.request,urllib.error,urllib.parse
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self,*a,**k):return None
        opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
        for port in (80,8080):
            for path in ('/health','/api/v1/auth/oidc/config','/api/v1/auth/oidc/callback'):
                request=urllib.request.Request('http://127.0.0.1:'+str(port)+path,headers={'Host':'knowhub.ruijie.com.cn'})
                try:response=opener.open(request,timeout=8)
                except urllib.error.HTTPError as error:response=error
                with response:
                    if response.headers.get('X-KnowHub-Environment')!=ENVIRONMENT_HEADER or response.code!=(302 if path.endswith('/callback') else 200):raise RuntimeError('company_route_mismatch')
                    if path.endswith('/callback'):
                        location=urllib.parse.urlsplit(response.headers.get('Location',''))
                        if location.hostname not in (None,'knowhub.ruijie.com.cn') or urllib.parse.parse_qs(location.fragment).get('oidc_error')!=['invalid_state']:raise RuntimeError('company_callback_mismatch')

    def rows(self,table,columns):
        return json.loads(self.query("SELECT COALESCE(json_agg(s),'[]') FROM (SELECT "+columns+' FROM '+table+' ORDER BY id) s'))

    def snapshot(self):
        # Limited permission metadata, never employee fields or document text.
        return {'users':self.rows('users','id,tenant_id,is_active,is_system_admin,can_access_all_tenants'),
                'members':self.rows('tenant_members','id,user_id,tenant_id,role,status,deleted_at'),
                'kbs':self.rows('knowledge_bases','id,tenant_id,deleted_at'),
                'acl':self.rows('kb_shares','id,knowledge_base_id,organization_id,source_tenant_id,permission,deleted_at'),
                'org_members':self.rows('organization_tenant_members','id,organization_id,tenant_id,role,representative_user_id'),
                'tenants':self.rows('tenants','id'),
                'sessions':self.rows('source_id_sessions','id,user_id,revoked'),
                'jit':self.rows('source_id_jit_registrations','id,user_id'),
                'ownership':self.rows('source_id_account_ownership','id,user_id,status'),
                'other_services':self.other_services()}

    def save_private_baseline(self,before):self.before=before  # RAM only.

    def transactions(self):
        # The f40 server stores version/start/prompt metadata before consuming.
        # Select only those metadata projections, not the raw result field.
        rows=json.loads(self.query("""SELECT COALESCE(json_agg(s),'[]') FROM (
        SELECT key,consumed,EXTRACT(EPOCH FROM expires_at) AS expires,
        CASE WHEN NOT consumed AND result LIKE '{"version":1,%' THEN EXTRACT(EPOCH FROM (result::jsonb->>'started')::timestamptz) ELSE NULL END AS started,
        CASE WHEN NOT consumed AND result LIKE '{"version":1,%' THEN COALESCE(result::jsonb->>'prompt','')='login' ELSE NULL END AS prompt
        FROM oidc_transactions ORDER BY expires_at LIMIT 10001) s"""))
        if len(rows)>10000:raise RuntimeError('transaction_snapshot_limit')
        out=[]
        for row in rows:
            key=row['key'];saved=self.tx_cache.get(key)
            if row['started'] is not None:
                saved=(float(row['started']),row['prompt']);self.tx_cache[key]=saved
            start,prompt=saved if saved is not None else (float(row['expires'])-600,False)
            # 600s is the local state lifetime in exact f40, NOT code issue time.
            out.append(Transaction(key,start,float(row['expires']),row['consumed'],prompt))
        return out

    def compose(self,override=False):
        args=DOCKER+['compose','-p',PROJECT,'--project-directory',str(ROOT)]
        for name in getattr(self,'base_compose',COMPOSE_FILES):args+=['-f',name]
        return args+(['-f',str(self.override)] if override else [])

    def recreate(self,override):
        command(self.compose(override)+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'],timeout=90)
        for _ in range(70):
            if inspect(APP,'.Image')==IMAGE and inspect(APP,'.State.Health.Status')=='healthy':break
            time.sleep(1)
        else:raise RuntimeError('app_health_failed')
        command(DOCKER+['exec',PROJECT+'-frontend-1','nginx','-s','reload'])

    def enable_approved(self,case):
        self.original_flags=self.flags()
        if getattr(self,'gateway_observation',None) is not None:self.gateway_observation.enable()
        atomic(self.override,{'services':{'app':{'environment':self.env}}})
        self.recreate(True)

    def restore_original_app_only(self):
        try:self.recreate(False)
        finally:
            if getattr(self,'gateway_observation',None) is not None:self.gateway_observation.restore()

    def start_stream(self):
        self.process=subprocess.Popen(DOCKER+['attach','--no-stdin','--sig-proxy=false',APP],stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        os.set_blocking(self.process.stdout.fileno(),False)
        self.stream_buffers={'app':b'','gateway':b''}
        if getattr(self,'gateway_observation',None) is not None:
            self.gateway_process=subprocess.Popen(DOCKER+['attach','--no-stdin','--sig-proxy=false',ENTRY],stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
            os.set_blocking(self.gateway_process.stdout.fileno(),False)

    def drain_lines(self):
        lines=[]
        for name,process in (('app',self.process),('gateway',getattr(self,'gateway_process',None))):
            if process is None:continue
            if process.poll() is not None:raise RuntimeError('live_stream_ended')
            buffer=self.stream_buffers[name]
            if select.select([process.stdout],[],[],0)[0]:buffer+=os.read(process.stdout.fileno(),262144)
            while b'\n' in buffer and len(lines)<256:
                line,buffer=buffer.split(b'\n',1)
                if len(line)>65536:raise RuntimeError('log_line_limit')
                lines.append(line.decode('utf-8','replace'))
            if len(buffer)>65536:raise RuntimeError('log_buffer_limit')
            self.stream_buffers[name]=buffer
        return lines

    def ready(self,run_id):
        self.run_id=run_id
        if getattr(self,'server_observer',None) is not None:
            # prepare.inventory already checked empty callback BEFORE attaching
            # the live stream; do not inject an unowned callback after READY.
            atomic(self.dir/'status.json',{'run_id':run_id,'phase':'READY','mode':'SERVER_METADATA_AND_USER_PORTAL_CONFIRMATION'})
            return
        atomic(self.observer_dir/'request.json',{'run_id':run_id,'case':self.case,'action':'observe_one_business_login'})
        # Observer is separately prepared in the authorized actual Chrome.
        path=self.observer_dir/'ready.json'
        for _ in range(20):
            if path.exists():
                v=protected(path)
                if set(v)=={'run_id','four_routes_verified','sso_context_confirmed'} and v['run_id']==run_id and v['four_routes_verified'] is True and v['sso_context_confirmed'] is True:
                    atomic(self.dir/'status.json',{'run_id':run_id,'phase':'READY'});return
            time.sleep(.25)
        raise RuntimeError('actual_browser_observer_not_ready')

    def validate_observation_resources(self):
        observer=self.server_observer
        ids=observer.allowed|({observer.denied} if observer.denied else set())
        literals=','.join("'"+x+"'" for x in ids)
        rows=json.loads(self.query("SELECT COALESCE(json_agg(id),'[]') FROM knowledge_bases WHERE deleted_at IS NULL AND id IN ("+literals+")"))
        if set(rows)!=ids:raise RuntimeError('approved_knowledge_resource_missing')
        docs=','.join("'"+x+"'" for x in observer.documents);kbs=','.join("'"+x+"'" for x in observer.allowed)
        rows=json.loads(self.query("SELECT COALESCE(json_agg(id),'[]') FROM knowledges WHERE deleted_at IS NULL AND id IN ("+docs+") AND knowledge_base_id IN ("+kbs+")"))
        if set(rows)!=observer.documents:raise RuntimeError('approved_document_resource_missing')

    def observe_business_lines(self,lines,m):
        observer=getattr(self,'server_observer',None)
        if observer is None:return
        for line in lines:observer.observe(line,m.stages['callback_result']=='PASS')
        if observer.failed:m.end('BUSINESS_INVARIANT_FAILURE')

    def session_candidate(self,before):
        rows=self.new_session_candidates(before)
        if len(rows)>1:raise RuntimeError('multiple_new_sessions')
        return rows[0] if rows else None

    def new_session_candidates(self,before):
        old={s['id'] for s in before['sessions']}
        return [s for s in self.rows('source_id_sessions','id,user_id,revoked') if s['id'] not in old]

    def candidate_belongs_to_run(self,s,m,before,case):
        if m.bound is None or not m.bound.consumed or len(m.seen)!=1 or not UUID.fullmatch(s['id']) or not UUID.fullmatch(s['user_id']):return False
        old={u['id'] for u in before['users']}
        if case=='existing':return s['user_id']==self.user and any(u['id']==self.user and u['is_active'] for u in before['users'])
        new={u['id'] for u in self.snapshot()['users']}-old
        jit=[j for j in self.rows('source_id_jit_registrations','id,user_id') if j['id'] not in {x['id'] for x in before['jit']}]
        return new=={s['user_id']} and len(jit)==1 and jit[0]['user_id']==s['user_id']

    def permissions_preserved(self,before,case):
        after=self.snapshot();old_ids={u['id'] for u in before['users']}
        return (all(before[k]==after[k] for k in ('members','kbs','acl','org_members','tenants','other_services')) and
                [u for u in after['users'] if u['id'] in old_ids]==before['users'] and
                len(after['users'])-len(before['users']) in ((0,) if case=='existing' else (0,1)))

    def business_probes(self,before,s,case,run_id,callback_trace):
        # Random internal IDs only, in the protected observer IPC directory.
        # The browser observer compares in-process and returns booleans; no
        # Cookie, JWT, employee number, email or claim is exported here.
        observer=getattr(self,'server_observer',None)
        if observer is not None:
            if not observer.complete():return None
            bound=json.loads(self.query("SELECT json_build_object('no_refresh',NOT EXISTS(SELECT 1 FROM auth_tokens WHERE session_id='"+s['id']+"' AND token_type='refresh_token'),'session_user_matches',EXISTS(SELECT 1 FROM source_id_sessions WHERE id='"+s['id']+"' AND user_id='"+s['user_id']+"'),'access_session_matches',EXISTS(SELECT 1 FROM auth_tokens WHERE session_id='"+s['id']+"' AND user_id='"+s['user_id']+"' AND token_type='access_token') AND NOT EXISTS(SELECT 1 FROM auth_tokens WHERE NOT is_revoked AND expires_at>NOW() AND (session_id IS DISTINCT FROM '"+s['id']+"' OR user_id<>'"+s['user_id']+"')),'logout_revoked',(SELECT revoked FROM source_id_sessions WHERE id='"+s['id']+"') AND NOT EXISTS(SELECT 1 FROM auth_tokens WHERE session_id='"+s['id']+"' AND NOT is_revoked))"))
            v={'callback_trace':callback_trace,'checks':observer.checks(bound)}
        else:
            atomic(self.observer_dir/'probe-request.json',{'run_id':run_id,'case':case,'callback_trace':callback_trace,'expected_user_id':s['user_id'],'expected_session_id':s['id']})
            path=self.observer_dir/'observations.json'
            if not path.exists():return None
            v=protected(path)
            if set(v)!={'run_id','callback_trace','checks'} or v['run_id']!=run_id or v['callback_trace']!=callback_trace:raise RuntimeError('observer_run_mismatch')
        self.observer_trace=v['callback_trace']
        check=v['checks'];browser={'auth_me_200','portal_loaded','no_refresh','knowledge_access_correct','logout_revoked','session_user_matches','access_session_matches'}
        if set(check)!=browser or any(type(x) is not bool for x in check.values()):raise RuntimeError('observer_schema_rejected')
        after=self.snapshot();uid=s['user_id'];new={u['id'] for u in after['users']}-{u['id'] for u in before['users']}
        q="SELECT json_build_object('local',COUNT(*)=1,'bounded',COALESCE(bool_and(NOT l.refresh_enabled AND l.access_seconds>0 AND l.idle_seconds>0 AND l.admission_valid_until>=x.expires_at AND NOT EXISTS(SELECT 1 FROM auth_tokens a WHERE a.session_id=x.id AND (a.token_type='refresh_token' OR a.expires_at>x.expires_at))),false)) FROM source_id_local_sessions l JOIN source_id_sessions x ON x.id=l.id WHERE l.id='"+s['id']+"'"
        policy=json.loads(self.query(q))
        if getattr(self,'env',{}).get('SOURCEID_ACCOUNT_LINK_MODE')=='verified_email_policy':
            # Only safe policy metadata, never UserInfo/employee claims. Exact
            # references are compared inside the process, not exported.
            markers=json.loads(self.query("SELECT COALESCE(json_agg(s),'[]') FROM (SELECT p.link_mode,p.policy_ref,p.binding_policy_ref FROM source_id_local_link_policies p JOIN source_id_link_decisions d ON d.id=p.decision_id JOIN source_id_binding_reviews r ON r.id=d.review_id WHERE p.id='"+s['id']+"' AND r.user_id='"+s['user_id']+"' AND r.status='approved') s"))
            if len(markers)!=1 or markers[0]!={'link_mode':'verified_email_policy','policy_ref':self.env['SOURCEID_VERIFIED_EMAIL_POLICY_REF'],'binding_policy_ref':self.env['SOURCEID_AUTO_BINDING_POLICY_REF']} or before['ownership']!=self.snapshot()['ownership']:raise RuntimeError('verified_email_policy_metadata_mismatch')
            self.link_policy_status='PASS'
        check.update(single_bound_session=len(self.new_session_candidates(before))==1 and policy['local'],session_access_bounded=policy['bounded'])
        preserved=self.permissions_preserved(before,case)
        if case=='existing':check.update(original_user_id=uid==self.user,roles_unchanged=preserved,memberships_unchanged=before['members']==after['members'],acl_unchanged=before['acl']==after['acl'],no_new_user=not new)
        else:
            u=next(x for x in after['users'] if x['id']==uid)
            # f40 stores tenantless users with TenantID=0 (uint64), not SQL NULL.
            zero=not u['is_system_admin'] and not u['can_access_all_tenants'] and u['tenant_id']==0
            check.update(one_new_user=new=={uid},single_identity_record=len([j for j in after['jit'] if j['user_id']==uid])==1,no_admin_flags=zero,no_memberships=not any(x['user_id']==uid for x in after['members']),no_default_knowledge_access=check['knowledge_access_correct'] and zero,no_historical_collision=preserved)
        return v['callback_trace'],check

    def operator_action(self,run_id):
        path=self.observer_dir/'operator.json'
        if not path.exists():return None
        v=protected(path,1024)
        observer=getattr(self,'server_observer',None)
        if observer is not None and set(v)=={'run_id','action','portal_confirmed'} and v['run_id']==run_id:
            observer.confirm_operator({k:v[k] for k in ('action','portal_confirmed')});return v['action']
        return v.get('action') if set(v)=={'run_id','action'} and v['run_id']==run_id else None

    def revoke_exact_session_and_access(self,s):
        self.query("UPDATE source_id_sessions SET revoked=TRUE WHERE id='"+s['id']+"' AND user_id='"+s['user_id']+"';UPDATE auth_tokens SET is_revoked=TRUE WHERE session_id='"+s['id']+"'",True)

    def revocation_verified(self,s):
        return self.query("SELECT (SELECT revoked FROM source_id_sessions WHERE id='"+s['id']+"') AND NOT EXISTS(SELECT 1 FROM auth_tokens WHERE session_id='"+s['id']+"' AND NOT is_revoked)")=='t'

    def invalidate_exact_unused(self,tx):
        if not re.fullmatch('[a-f0-9]{64}',tx.handle):raise RuntimeError('invalid_private_transaction_handle')
        current=next((x for x in self.transactions() if x.handle==tx.handle),None)
        if current is None or current.consumed:return 'NO_UNUSED_BOUND_TRANSACTION'
        if current.expires!=tx.expires:raise RuntimeError('transaction_changed')
        self.query("UPDATE oidc_transactions SET consumed=TRUE,result='',nonce='',verifier='' WHERE key='"+tx.handle+"' AND NOT consumed AND expires_at=to_timestamp("+str(tx.expires)+")",True)
        if any(x.handle==tx.handle and not x.consumed for x in self.transactions()):raise RuntimeError('transaction_cleanup_not_confirmed')
        return 'EXACT_UNUSED_TRANSACTION_INVALIDATED'

    def save_safe(self,name,result):atomic(self.dir/(name+'.json'),result)
    def close_stream(self):
        for process in (getattr(self,'process',None),getattr(self,'gateway_process',None)):
            if process:
                process.terminate()
                try:process.wait(timeout=5)
                except subprocess.TimeoutExpired:process.kill();process.wait()
                process.stdout.close()


def main(argv=None):
    p=argparse.ArgumentParser();p.add_argument('--execute-authorized',action='store_true');p.add_argument('--exclusive-window-confirmed',action='store_true');p.add_argument('--case',choices=('existing','jit'));p.add_argument('--protected-inputs',type=Path)
    a=p.parse_args(argv)
    if not a.execute_authorized or not a.exclusive_window_confirmed:
        print(json.dumps({'result':'NOT_EXECUTED','real_business_authorization_required':True,'new_sid_requests':0}));return 0
    if a.case is None or a.protected_inputs is None:p.error('protected inputs and case required')
    import fcntl
    import signal
    def interrupted(signum,frame):raise KeyboardInterrupt()
    signal.signal(signal.SIGTERM,interrupted);signal.signal(signal.SIGINT,interrupted)
    os.umask(0o077)
    with (RELEASE/'private/business-monitor.lock').open('a') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
        port=DockerPilotPort(protected(a.protected_inputs),a.case)
        result=run(port,a.case,True,True)
        print(json.dumps({k:result.get(k) for k in ('run_id','case','result','reason','recovery')}))
    return 0


if __name__=='__main__':
    try:raise SystemExit(main())
    except Exception:
        print(json.dumps({'result':'FAIL','reason':'CONTROLLED_EXECUTION_FAILURE','raw_error_saved':False}));raise SystemExit(2)
