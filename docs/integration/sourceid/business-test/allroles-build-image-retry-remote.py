"""Use verified local runtime tag and legacy builder; no registry lookup."""
import os
b=pathlib.Path('/home/aqyfzx/ai/knowhub-allroles-035f35e1d')
image='sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1'
tag='knowhub-allroles/local-runtime:431226c3b'
run(['docker','tag',image,tag])
assert run(['docker','image','inspect','--format','{{.Id}}',tag]).decode().strip()==image
p=b/'Dockerfile';p.write_text(p.read_text().replace('FROM '+image,'FROM '+tag))
(b/'.dockerignore').write_text('private\nsource.tar.gz\nsource\nworker.log\nartifacts/*.log\n')
env=dict(os.environ,DOCKER_BUILDKIT='0')
with (b/'artifacts/image-retry.log').open('wb') as f:
    q=subprocess.run(['docker','build','--pull=false','--network','none','-t','knowhub-allroles/app:035f35e1d','-f',str(p),str(b)],stdout=f,stderr=subprocess.STDOUT,env=env,timeout=90)
assert q.returncode==0
iid=run(['docker','image','inspect','--format','{{.Id}}','knowhub-allroles/app:035f35e1d']).decode().strip()
state={'result':'PASS','stage':'complete','revision':'035f35e1d388a2f02615d35cf826bbdcc22e56b0','image_id':iid,'runtime_image_id':image,'compiler':'go1.26.7','live_services_changed':False}
(b/'status.json').write_text(json.dumps(state,indent=2))
print(json.dumps(state))
