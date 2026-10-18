R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');I='sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b'
code="""import os,subprocess,urllib.parse,json,re
dsn='postgres://fresh_owner:'+urllib.parse.quote(os.environ['DB_PASSWORD'],safe='')+'@postgres:5432/knowhub_jit_fresh?sslmode=disable'
p=subprocess.run(['migrate','-path','/migration','-database',dsn,'goto','3030'],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
text=p.stderr.decode().lower()
markers=['permission denied','no such file','no migration','no version','unknown driver','connection refused','lookup','password authentication failed','does not exist','unable to open','no route','network is unreachable','dirty','syntax error','requires','extension','already exists','duplicate','null','unique','foreign key']
print(json.dumps({'exit':p.returncode,'categories':[v for v in markers if v in text],'sqlstates':re.findall(r'(?i)sqlstate[ :]+([A-Z0-9]{5})',text),'migration_versions':re.findall(r'(?i)(?:version|migration)[: ]+(-?[0-9]+)',text),'stderr_length':len(p.stderr)}))
"""
print(run(['docker','run','--rm','--network','knowhub-jit-fresh_isolated','--log-driver','none','--env-file',str(R/'private/app.env'),'--mount','type=bind,source=/home/aqyfzx/ai/knowhub-verified-email-implementation-20261017/source/migrations/versioned,target=/migration,readonly','--entrypoint','python3',I,'-c',code],timeout=300).decode())
