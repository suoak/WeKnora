"""Build complete APP/frontend images from one verified frozen worktree."""
from pathlib import Path
import datetime,hashlib,json,shutil,subprocess,sys

base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-mcp-v1-20261021')
m=json.loads((base/'manifest.json').read_text());sha=m['candidate_sha256'];assert base.name==sha[:16]
out=base/'results'/'build';out.mkdir(exist_ok=True)
results=json.loads((out/'results.json').read_text()) if (out/'results.json').exists() else []
def run(name,args,timeout=1800):
    previous=[r for r in results if r['name']==name or r['name'].startswith(name+'-retry-')]
    if previous and previous[-1]['exit_code']==0:return
    if previous:name=name+'-retry-'+str(len(previous))
    row={'name':name,'argv':args,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
    with (out/(name+'.log')).open('wb')as f:
        try:row['exit_code']=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
        except subprocess.TimeoutExpired:row['exit_code']=124
    row['finished_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();results.append(row)
    (out/'results.json').write_text(json.dumps(results,indent=2));(out/(name+'.exit.txt')).write_text(str(row['exit_code']))
    assert row['exit_code']==0,name

native=Path('/home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go')
library=native/'lib/linux_amd64_gnu/libanydoc_go.a'
assert hashlib.sha256(library.read_bytes()).hexdigest()=='d5543b2cf68400c048992a192475aa70e895fda0abc495b85afb68dfd699122a'
# Reuse only the unchanged, pinned native dependency, never an old APP binary.
for n,h in m['files'].items():
    if n.startswith('third_party/anydoc-go/') and not n.endswith(('.md','.gitignore')):
        other=native/Path(n).relative_to('third_party/anydoc-go')
        assert other.is_file() and other.read_bytes().replace(b'\r\n',b'\n')==(base/'source'/n).read_bytes().replace(b'\r\n',b'\n'),n
(out/'native-material.json').write_text(json.dumps({'source':str(native),'sha256':hashlib.sha256(library.read_bytes()).hexdigest(),'source_inputs_verified':True}))
volume='knowhub-mcp-v1-dev-'+sha[:16]
common=['docker','run','--rm','--label','knowhub.v1=build','--network','none',
        '-v',str(base/'source')+':/source:ro','-v',str(library.parent)+':/native:ro',
        '-v',str(base/'artifacts')+':/artifacts','-v',volume+':/v1',
        '-v','knowhub-mcp-b7-20261021-102200-data:/prior',
        '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro',
        '-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/prior/go-cache','-e','GOTMPDIR=/v1/tmp',
        '-e','GOTOOLCHAIN=local','-e','CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers',
        '-e','CGO_LDFLAGS=-L/native',
        '-w','/source','knowhub-build/golang:1.26-bookworm-ustc']
run('build-init',common+['sh','-c','mkdir -p /v1/tmp /v1/sqlite-headers; cp /home/aqyfzx/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.24/sqlite3-binding.h /v1/sqlite-headers/sqlite3.h; cp /home/aqyfzx/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.24/sqlite3ext.h /v1/sqlite-headers/'])
ldflags='-X github.com/Tencent/WeKnora/internal/handler.Version=V1-candidate -X github.com/Tencent/WeKnora/internal/handler.CommitID=worktree-'+sha[:16]+' -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn'
run('app-native-build',common+['go','build','-tags','sqlite_fts5,anydoc','-ldflags',ldflags,'-o','/artifacts/WeKnora','./cmd/server'])
run('seed-build',common+['go','test','-tags','sqlite_fts5,anydoc,knowhub_v1_deployed','-c','-o','/artifacts/seed.test','./internal/router'])
# Frontend is built in a Linux workspace copied from the same frozen source.
node=['docker','run','--rm','--label','knowhub.v1=build','--network','bridge',
      '-v',str(base/'source/frontend')+':/source:ro','-v',str(base/'artifacts')+':/artifacts','-v',volume+':/v1',
      '-e','NODE_OPTIONS=--max-old-space-size=4096','-e','VITE_IS_DOCKER=true',
      '-e','VITE_FRONTEND_COMMIT=worktree-'+sha[:16],'-w','/v1/frontend','node:22-bookworm']
run('frontend-source',node[:-3]+['-w','/v1','node:22-bookworm','sh','-c','mkdir -p /v1/frontend; cp -a /source/. /v1/frontend/'])
if (base/'artifacts/xlsx-lockfile.tgz').exists():
    run('lockfile-cache-import',node+['npm','cache','add','/artifacts/xlsx-lockfile.tgz','--cache','/v1/npm-cache'])
run('frontend-dependencies',node+['npm','ci','--prefer-offline','--cache','/v1/npm-cache','--registry=https://registry.npmmirror.com'],1200)
run('frontend-types',node+['node','node_modules/vue-tsc/bin/vue-tsc.js','--build','--pretty','false'])
run('frontend-build',node+['node','node_modules/vite/bin/vite.js','build','--configLoader','runner','--outDir','/artifacts/dist'])
appbase='knowhub-rc3/app:0.8.0-oa-feishu-rc3-1e2ded5b6'
assert json.loads(subprocess.check_output(['docker','image','inspect',appbase]))[0]['Id']=='sha256:0c84cac5af5103b73102c6261f32130d3e51eeed1b8cbf77fda61de20c4bd983'
assert json.loads(subprocess.check_output(['docker','image','inspect','nginx:1.30.3-alpine']))[0]['Id']=='sha256:d0701bd41f8279135aa6c81e23fbcdae7bcc3df591c47741405412a8dcdb2eab'
run('runtime-material',['docker','image','inspect',appbase,'nginx:1.30.3-alpine','node:22-bookworm'])
context=base/'image-context';context.mkdir(exist_ok=True)
shutil.copy2(base/'artifacts/WeKnora',context/'WeKnora')
for directory in ['config','migrations','scripts']:
    shutil.copytree(base/'source'/directory,context/directory,dirs_exist_ok=True)
for script in (context/'scripts').rglob('*.sh'):
    script.write_bytes(script.read_bytes().replace(b'\r\n',b'\n'))
appimage='knowhub-v1/app:'+sha[:16];frontimage='knowhub-v1/frontend:'+sha[:16]
(context/'Dockerfile.app').write_text('FROM '+appbase+'\nCOPY --chmod=0755 WeKnora /app/WeKnora\nCOPY config /app/config\nCOPY migrations /app/migrations\nCOPY --chmod=0755 scripts /app/scripts\nCOPY jieba-dict /app/jieba-dict\nENV JIEBA_DICT_DIR=/app/jieba-dict\nLABEL knowhub.source.sha256="'+sha+'" knowhub.release="V1-isolated-candidate"\n')
shutil.copytree(base/'artifacts/dist',context/'dist',dirs_exist_ok=True)
shutil.copytree('/home/aqyfzx/go/pkg/mod/github.com/yanyiwu/gojieba@v1.4.7/deps/cppjieba/dict',context/'jieba-dict',dirs_exist_ok=True)
for name in ['nginx.conf','nginx-api-proxy.conf','nginx-http.conf','docker-entrypoint.sh']:
    shutil.copy2(base/'source/frontend'/name,context/name)
(context/'docker-entrypoint.sh').write_bytes((context/'docker-entrypoint.sh').read_bytes().replace(b'\r\n',b'\n'))
(context/'Dockerfile.frontend').write_text('FROM nginx:1.30.3-alpine\nCOPY dist /usr/share/nginx/html\nCOPY nginx.conf /etc/nginx/templates/default.conf.template\nCOPY nginx-api-proxy.conf /etc/nginx/api-proxy.conf\nCOPY nginx-http.conf /etc/nginx/conf.d/00-weknora-http.conf\nCOPY --chmod=0755 docker-entrypoint.sh /docker-entrypoint.sh\nENV MAX_FILE_SIZE_MB=100\nENTRYPOINT ["/docker-entrypoint.sh"]\nLABEL knowhub.source.sha256="'+sha+'"\n')
run('app-image-runtime',['docker','build','--network','none','--pull=false','-f',str(context/'Dockerfile.app'),'-t',appimage,str(context)])
run('frontend-image',['docker','build','--network','none','--pull=false','-f',str(context/'Dockerfile.frontend'),'-t',frontimage,str(context)])
images=json.loads(subprocess.check_output(['docker','image','inspect',appimage,frontimage]))
(out/'images.json').write_text(json.dumps([{'image':i['RepoTags'],'id':i['Id'],'repo_digests':i['RepoDigests'],'source':i['Config']['Labels'].get('knowhub.source.sha256')}for i in images],indent=2))
(out/'artifacts.json').write_text(json.dumps({str(p.relative_to(base/'artifacts')):hashlib.sha256(p.read_bytes()).hexdigest()for p in (base/'artifacts').rglob('*')if p.is_file()},indent=2))
(out/'image-context.json').write_text(json.dumps({'script_normalization':'CRLF to LF only; frozen source remains unchanged','files':{str(p.relative_to(context)):hashlib.sha256(p.read_bytes()).hexdigest()for p in context.rglob('*')if p.is_file()}},indent=2))
(out/'done.json').write_text(json.dumps({'candidate_sha256':sha,'all_passed':True,'app':appimage,'frontend':frontimage,'build_tags':'sqlite_fts5,anydoc'}))
