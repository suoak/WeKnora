"""Observe existing APP access metadata; never request/read browser credentials.

Portal rendering is explicit user observation, not inferred from a static 200.
Raw APP lines are examined in RAM only and are never returned or persisted.
"""
import ipaddress,re,uuid,json
ANSI=re.compile(r'\x1b\[[0-9;]*m')
UUID=r'[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}'
class ServerBusinessObserver:
    def __init__(self,source_ip,allowed_kbs,documents,denied_kb,require_negative=True):
        self.source=ipaddress.ip_address(source_ip)
        self.allowed=set(allowed_kbs);self.documents=set(documents);self.denied=denied_kb
        ids=self.allowed|self.documents|({denied_kb} if denied_kb else set())
        if not self.allowed or not self.documents or any(str(uuid.UUID(x))!=x for x in ids):raise ValueError('approved_resource_ids_required')
        if denied_kb in self.allowed:raise ValueError('allow_deny_resource_conflict')
        if type(require_negative) is not bool:raise ValueError('invalid_acceptance_scope')
        self.require_negative=require_negative
        self.seen=set();self.request_ids=set();self.portal_confirmed=False;self.operator_complete=False;self.failed=False
    def observe(self,line,callback_complete=False):
        if not callback_complete or not isinstance(line,str) or len(line)>65536:return
        if line.startswith('{'):
            try:value=json.loads(line)
            except ValueError:return
            if set(value)!={'event','kind','status','method','source_approved','request_id'} or value['event']!='company_business_http' or value['source_approved'] is not True:return
            if not isinstance(value['request_id'],str) or not re.fullmatch('[a-f0-9]{32}',value['request_id']) or type(value['status']) is not int:return
            if value['request_id'] in self.request_ids:return
            expected={'auth_me':('GET',200),'logout':('POST',200),'allowed_kb':('GET',200),'denied_kb':('GET',403),'document':('GET',200)}
            if value['kind'] not in expected or value['method']!=expected[value['kind']][0]:return
            if value['kind']=='auth_me' and value['status']==401 and 'logout' not in self.seen:self.failed=True
            if value['kind']=='denied_kb' and value['status']!=403:self.failed=True
            if value['status']==expected[value['kind']][1] or value['kind']=='logout' and value['status']==204:
                self.seen.add(value['kind']);self.request_ids.add(value['request_id'])
            return
        text=ANSI.sub('',line)
        # Match the formatter's metadata prefix before any request/response body.
        pos=text.find(' client_ip=')
        if pos<0:return
        traces=re.findall(UUID,text[:pos]);
        if not traces:return
        fields=text[pos+1:]
        if fields.count(' latency=')!=1:return
        prefix=re.match(r'client_ip=([^ ]+) (?:error=.*? )?latency=[^ ]+ method=(GET|POST) path=([^ ]+)',fields)
        suffix=re.search(r' size=-?[0-9]+ status_code=([0-9]{3})\][^|]*\|\s*$',fields)
        if not prefix or not suffix:return
        try:
            if ipaddress.ip_address(prefix[1])!=self.source:return
        except ValueError:return
        trace=traces[-1]
        if trace in self.request_ids:return
        method=prefix[2];path=prefix[3].split('?',1)[0];status=int(suffix[1])
        if self.denied and path=='/api/v1/knowledge-bases/'+self.denied and status!=403:self.failed=True
        if path=='/api/v1/auth/me' and status==401 and 'logout' not in self.seen:self.failed=True
        category=None
        if path=='/api/v1/auth/me' and method=='GET' and status==200:category='auth_me'
        elif path=='/api/v1/auth/logout' and method=='POST' and status in (200,204):category='logout'
        elif method=='GET':
            if path in {'/api/v1/knowledge-bases/'+x for x in self.allowed} and status==200:category='allowed_kb'
            elif self.denied and path=='/api/v1/knowledge-bases/'+self.denied and status==403:category='denied_kb'
            elif any(path in ('/api/v1/knowledge/'+x,'/api/v1/knowledge/'+x+'/download','/api/v1/knowledge/'+x+'/preview','/api/v1/chunks/'+x) for x in self.documents) and status==200:category='document'
        if category:self.seen.add(category);self.request_ids.add(trace)
    def confirm_operator(self,value):
        if set(value)=={'action','portal_confirmed'} and value['action']=='authentication_complete' and type(value['portal_confirmed']) is bool:
            self.operator_complete=True;self.portal_confirmed=value['portal_confirmed']
    def checks(self,db):
        return {'auth_me_200':'auth_me' in self.seen,'portal_loaded':self.portal_confirmed,
                'no_refresh':db['no_refresh'],'knowledge_access_correct':({'allowed_kb','document','denied_kb'} if self.require_negative else {'allowed_kb','document'})<=self.seen,
                'logout_revoked':'logout' in self.seen and db['logout_revoked'],
                'session_user_matches':db['session_user_matches'],'access_session_matches':db['access_session_matches']}
    def complete(self):return self.portal_confirmed and self.operator_complete and ({'auth_me','allowed_kb','document','denied_kb','logout'} if self.require_negative else {'auth_me','allowed_kb','document','logout'})<=self.seen
