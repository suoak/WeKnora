"""Temporary safe gateway access metadata, no bodies/headers/query/IP logging."""
import ipaddress,re
from pathlib import Path
ENTRY='knowhub-company-entry-entry-1'
CONFIG=Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/entry/nginx.conf')
ID=re.compile(r'[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}')
def configuration(original,source,allowed,documents,denied):
    source=str(ipaddress.ip_address(source))
    if 'allow '+source+';' not in original or original.count('access_log off;')!=1 or 'company_business_http' in original:raise ValueError('approved_entry_configuration_required')
    entries=[('/api/v1/auth/me','auth_me'),('/api/v1/auth/logout','logout')]
    for uid in set(allowed)|set(documents)|({denied} if denied else set()):
        if not isinstance(uid,str) or not ID.fullmatch(uid):raise ValueError('approved_resource_id_required')
    entries += [('/api/v1/knowledge-bases/'+x,'allowed_kb') for x in allowed]
    if denied:entries += [('/api/v1/knowledge-bases/'+denied,'denied_kb')]
    for x in documents:
        entries += [(p,'document') for p in ('/api/v1/knowledge/'+x,'/api/v1/knowledge/'+x+'/preview','/api/v1/knowledge/'+x+'/download','/api/v1/chunks/'+x)]
    mapping=' '.join('"'+path+'" '+kind+';' for path,kind in entries)
    block=('map $uri $kh_kind { default ""; '+mapping+' } '
           'map $kh_kind $kh_emit { default 1; "" 0; } '
           'map $remote_addr $kh_source { default false; '+source+' true; } '
           'log_format kh_business escape=json \'{"event":"company_business_http","kind":"$kh_kind","status":$status,"method":"$request_method","source_approved":$kh_source,"request_id":"$request_id"}\'; '
           'access_log /dev/stdout kh_business if=$kh_emit;')
    return original.replace('access_log off;',block,1)
class GatewayObservation:
    def __init__(self,command,inspect,source,allowed,documents,denied):
        self.command=command;self.inspect=inspect;self.source=source;self.allowed=allowed;self.documents=documents;self.denied=denied;self.original=None
    def enable(self):
        if self.inspect(ENTRY,'.HostConfig.LogConfig').get('Type')!='none':raise RuntimeError('gateway_raw_persistence_not_allowed')
        self.original=CONFIG.read_bytes()
        updated=configuration(self.original.decode(),self.source,self.allowed,self.documents,self.denied)
        # Keep bind-mounted inode. Atomic replacement would leave the container
        # looking at the previous inode instead of this reviewed configuration.
        try:
            CONFIG.write_text(updated);self.command(['docker','--host','unix:///var/run/docker.sock','exec',ENTRY,'nginx','-t']);self.command(['docker','--host','unix:///var/run/docker.sock','exec',ENTRY,'nginx','-s','reload'])
        except Exception:
            self.restore();raise
    def restore(self):
        if self.original is not None:
            CONFIG.write_bytes(self.original)
            self.command(['docker','--host','unix:///var/run/docker.sock','exec',ENTRY,'nginx','-t']);self.command(['docker','--host','unix:///var/run/docker.sock','exec',ENTRY,'nginx','-s','reload'])
            if CONFIG.read_bytes()!=self.original:raise RuntimeError('gateway_observation_restore_failed')
            self.original=None
