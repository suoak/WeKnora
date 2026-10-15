"""Build frontend candidate only, after exact-SHA CI success. No deployment."""
import hashlib
import os
import tarfile
os.umask(0o077)
base = pathlib.Path(UI_BUILD_DIRECTORY)
manifest = json.loads((base / 'source-manifest.json').read_text())
sha = manifest['sha']
assert re.fullmatch(r'[0-9a-f]{40}', sha)
assert manifest['ci_sha'] == sha and manifest['ci_conclusion'] == 'success'
assert base == pathlib.Path('/home/aqyfzx/ai/knowhub-oa-ui-candidate-' + sha[:9])
assert hashlib.sha256((base / 'source.tar.gz').read_bytes()).hexdigest() == manifest['archive_sha256']
source = base / 'source'
source.mkdir(mode=0o700, exist_ok=False)
with tarfile.open(str(base / 'source.tar.gz')) as archive:
    for member in archive.getmembers():
        (source / member.name).resolve().relative_to(source.resolve())
        if member.issym() or member.islnk():
            (source / member.name).parent.joinpath(member.linkname).resolve().relative_to(source.resolve())
    archive.extractall(str(source))
old = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b')
for name in ('package.json', 'package-lock.json'):
    assert (source / 'frontend' / name).read_bytes() == (old / 'source/frontend' / name).read_bytes()
(base / 'artifacts').mkdir(mode=0o700)
script = (old / 'build-frontend.sh').read_text().replace('431226c3b', sha[:9])
(base / 'build-frontend.sh').write_text(script)
dockerfile = (old / 'Dockerfile.frontend').read_text().replace('431226c3b1bd5338ec06c1bd0520e637d5fed398', sha)
dockerfile += '\nLABEL knowhub.change_scope="oa-login-ui-only" knowhub.compatible_app_revision="431226c3b1bd5338ec06c1bd0520e637d5fed398"\n'
(base / 'Dockerfile.frontend').write_text(dockerfile)
(base / '.dockerignore').write_text('source.tar.gz\nartifacts/*.log\nsource/frontend/node_modules\nsource/frontend/dist\n')
builder = 'sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb'
assert run(['docker', 'image', 'inspect', '--format', '{{.Id}}', builder]).decode().strip() == builder
cache = pathlib.Path('/home/aqyfzx/ai/knowhub-feishu-personal-20261014')
tag = 'knowhub-sourceid-phaseb/frontend:oa-ui-' + sha[:9]
args = ['docker', 'run', '--rm', '--name', 'knowhub-oa-ui-build-' + sha[:9], '--network', 'none', '--cpus', '2', '--memory', '8g',
        '-v', str(base) + ':/work', '-v', str(cache / 'tools') + ':/work/tools:ro',
        '-v', str(cache / 'toolchain') + ':/work/toolchain:ro', '-v', str(cache / 'cache') + ':/work/cache',
        '--entrypoint', 'bash', builder, '/work/build-frontend.sh']
worker = """import subprocess,pathlib,json,datetime
base=pathlib.Path(BASE)
def state(value): (base/'build-status.json').write_text(json.dumps({'state':value,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sha':SHA}))
for label,args in COMMANDS:
 state(label)
 with (base/'artifacts'/('controller-'+label+'.log')).open('wb') as f:
  result=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT)
 if result.returncode: state('FAILED');raise SystemExit(1)
state('COMPLETE')
""".replace('pathlib.Path(BASE)', 'pathlib.Path(' + repr(str(base)) + ')').replace("'sha':SHA", "'sha':" + repr(sha)).replace('in COMMANDS:', 'in ' + repr([
    ('frontend', args), ('image', ['docker', 'build', '--pull=false', '--network', 'none', '-f', str(base / 'Dockerfile.frontend'), '-t', tag, str(base)])]) + ':')
(base / 'worker.py').write_text(worker)
with (base / 'worker.log').open('wb') as log:
    p = subprocess.Popen(['python3', str(base / 'worker.py')], cwd=str(base), stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
print(json.dumps({'build_started': True, 'directory': str(base), 'pid': p.pid, 'sha': sha, 'frontend_tag': tag,
                  'app_build': 'NOT_EXECUTED', 'running_services_changed': False}))
