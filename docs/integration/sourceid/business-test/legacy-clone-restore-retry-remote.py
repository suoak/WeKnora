"""Retry restore only in dedicated clone, cleaning preinstalled schema objects."""
import importlib.util
s=importlib.util.spec_from_file_location('legacy','/home/aqyfzx/ai/knowhub-legacy79-worker-20261016.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
assert inspect(m.PG,'.HostConfig.NetworkMode')=='none'
assert inspect(m.PG,'.Config.Labels')['knowhub.test']=='legacy79-rehearsal'
with (m.BASE/'private/database79.dump').open('rb') as f:
    p=subprocess.run(['docker','exec','-i',m.PG,'pg_restore','-U','clone','-d','clone','--clean','--if-exists','--no-owner','--no-acl','--exit-on-error'],stdin=f,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=100)
out={'result':'PASS' if p.returncode==0 else 'FAIL','restore_clean_if_exists':True,'clone_only':True}
if p.returncode:
    raw=p.stderr.decode('utf-8','replace').lower()
    out['markers']=[w for w in ('extension','already exists','does not exist','relation','schema','function','role','access method','permission denied','unrecognized configuration parameter','must be owner','foreign key') if w in raw]
else:
    out['schema']=m.sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")
(m.BASE/'restore-retry.json').write_text(json.dumps(out,indent=2))
print(json.dumps(out))
