"""Export only synthetic test names/counts and immutable candidate metadata."""
b=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-479416c90')
s=json.loads((b/'status.json').read_text());assert s['result']=='PASS'
log=(b/'artifacts/build.log').read_text(errors='replace')
packages=re.findall(r'^ok\s+([^\s]+)\s+([0-9.]+s)',log,re.M)
assert len(packages)==6 and not re.search(r'^FAIL\s|^--- FAIL:',log,re.M)
assert pathlib.Path(b/'artifacts/WeKnora').stat().st_size>1000000
print(json.dumps({'result':'PASS','source_sha':s['revision'],'image_id':s['image_id'],'toolchain':'go1.26.7','network':'none','race_enabled':True,'package_results':[{'package':p,'elapsed':v} for p,v in packages],'test_filter':'SourceID|OIDC|AuthToken|SwitchTenant|Login|Logout|GenerateTokens|ValidateToken|RefreshToken','binary_built':True,'real_credentials_used':False,'live_services_changed':False}))
