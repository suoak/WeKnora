"""Complete runtime artifact packaging; preserve compiled binaries/toolchain."""
import os,shutil,time
os.umask(0o077)
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-candidate-09e6d86db');p=pathlib.Path('/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a')
receipt=json.loads((b/'build-status.json').read_text());assert receipt['result']=='PASS'
old=receipt['image_id'];assert inspect('knowhub-oa-sso-pilot-app-1','.Image')==old
shutil.copyfile(b/'build-status.json',b/'build-binary-only-receipt.json')
shutil.copytree(b/'source/migrations',b/'artifacts/migrations',dirs_exist_ok=True)
dockerfile=(b/'Dockerfile.app').read_text();assert 'COPY artifacts/migrations' not in dockerfile
dockerfile+='COPY artifacts/migrations /app/migrations\n';(b/'Dockerfile.app').write_text(dockerfile)
with (b/'artifacts/package-migrations.log').open('wb') as f:
 z=subprocess.run(['docker','build','--pull=false','--network','none','-f',str(b/'Dockerfile.app'),'-t','knowhub-enterprise/app:09e6d86db',str(b)],stdout=f,stderr=subprocess.STDOUT,timeout=120);assert z.returncode==0
image=json.loads(run(['docker','image','inspect','knowhub-enterprise/app:09e6d86db']))[0];new=image['Id'];assert image['Config']['Labels']['org.opencontainers.image.revision']==receipt['revision']
code="import pathlib,hashlib,json;print(json.dumps({p:hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest() for p in ['/app/WeKnora','/app/sourceid-ownership']}))"
before=json.loads(run(['docker','run','--rm','--network','none','--log-driver','none','--entrypoint','python3',old,'-c',code]));after=json.loads(run(['docker','run','--rm','--network','none','--log-driver','none','--entrypoint','python3',new,'-c',code]));assert before==after
probe="from pathlib import Path;assert Path('/app/migrations/versioned/003028_sourceid_enterprise_ownership.up.sql').is_file();assert Path('/app/migrations/sqlite/003028_sourceid_enterprise_ownership.up.sql').is_file()"
run(['docker','run','--rm','--network','none','--log-driver','none','--entrypoint','python3',new,'-c',probe])
receipt.update(image_id=new,packaging_completed_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),migration_003028_packaged=True,compiled_binaries_unchanged=True)
(b/'build-status.json').write_text(json.dumps(receipt,indent=2))
pin=b/'enterprise-app-pin.json';pin.write_text(json.dumps({'services':{'app':{'image':new}}},indent=2))
run(['docker','compose','-p','knowhub-oa-sso-pilot','--project-directory',str(p),'-f',str(p/'compose.json'),'-f',str(pin),'up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'],timeout=60)
for _ in range(65):
 if inspect('knowhub-oa-sso-pilot-app-1','.State.Health.Status')=='healthy':break
 time.sleep(1)
else:raise RuntimeError('repackaged_app_health_failed')
run(['docker','exec','knowhub-oa-sso-pilot-frontend-1','nginx','-s','reload'])
assert inspect('knowhub-oa-sso-pilot-app-1','.Image')==new
upgrade=json.loads((b/'upgrade-result.json').read_text());upgrade['initial_binary_only_image']=upgrade['image_id'];upgrade['image_id']=new;upgrade['migration_003028_packaged']=True;(b/'upgrade-result.json').write_text(json.dumps(upgrade,indent=2))
print(json.dumps(receipt))
