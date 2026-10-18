R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');APP='knowhub-jit-fresh-postgres-1'
def query(sql):
 z=subprocess.run(['docker','exec','-i',APP,'psql','-X','-qAt','-U','fresh_owner','-d','knowhub_jit_fresh','-v','ON_ERROR_STOP=1'],input=sql.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 return {'exit':z.returncode,'safe_output':z.stdout.decode().strip()}
out={'schema_exists':query("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='schema_migrations'"),'schema':query('SELECT version::text||\'|\'||dirty FROM schema_migrations'),'containers':{n:inspect('knowhub-jit-fresh-'+n+'-1','.State.Status') for n in ('postgres','redis','docreader')}}
# Driver flags only; no credentials or migration retry.
out['migration_binary']=run(['docker','run','--rm','--network','none','--log-driver','none','--entrypoint','sh','sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b','-c','command -v migrate']).decode().strip()
print(json.dumps(out))
