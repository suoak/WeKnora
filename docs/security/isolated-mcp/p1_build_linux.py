"""Bounded checks on the explicitly approved disposable packaging host tree."""
import datetime
import json
import os
import shutil
from pathlib import Path
import subprocess
import sys

base = Path(sys.argv[1]).resolve()
assert base == Path('/home/aqyfzx/ai/knowhub-isolated-p1-20261111')
source = base / 'source'
results = base / 'results'
results.mkdir(exist_ok=True)
env = dict(os.environ, GOMAXPROCS='2', GOTOOLCHAIN='local',
           GOPROXY='off', GOSUMDB='off', CGO_ENABLED='1')
rows = []


def run(name, args, seconds=1200):
    row = {'name': name, 'started_utc': datetime.datetime.now(datetime.timezone.utc).isoformat()}
    with (results / (name + '.log')).open('wb') as output:
        try:
            row['exit_code'] = subprocess.run(args, cwd=source, env=env, stdout=output,
                                              stderr=subprocess.STDOUT, timeout=seconds).returncode
        except subprocess.TimeoutExpired:
            row['exit_code'] = 124
    rows.append(row)
    (results / 'build-results.json').write_text(json.dumps(rows, indent=2))
    return row['exit_code']


go = '/snap/bin/go'
run('core', [go, 'test', '-tags', 'sqlite_fts5', '-p', '2', '-count=1',
             './internal/mcpserver', './internal/middleware', './internal/logger',
             './internal/utils', './internal/perftrace'])
run('embedding-privacy', [go, 'test', '-tags', 'sqlite_fts5', '-p', '2', '-count=1',
                        './internal/models/embedding', '-run', 'TestPersonalPayload'])
run('fixture-build', [go, 'test', '-tags', 'sqlite_fts5,knowhub_isolated_p1', '-p', '2',
                      '-c', '-o', str(base / 'fixture.test'), './internal/router'])
# The packaging host's GCC 9 cannot link the baseline DuckDB C++ archive.
# Use the existing approved Bookworm build image, without changing host tools.
goroot = subprocess.check_output([go, 'env', 'GOROOT']).decode().strip()
gocache = subprocess.check_output([go, 'env', 'GOCACHE']).decode().strip()
headers = base / 'sqlite-headers'
headers.mkdir(exist_ok=True)
sqlite = Path('/home/aqyfzx/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.24')
shutil.copyfile(sqlite / 'sqlite3-binding.h', headers / 'sqlite3.h')
shutil.copyfile(sqlite / 'sqlite3ext.h', headers / 'sqlite3ext.h')
app_build = ['docker', 'run', '--rm', '--network', 'none',
             '--user', str(os.getuid()) + ':' + str(os.getgid()),
             '-v', str(base) + ':' + str(base), '-v', goroot + ':' + goroot + ':ro',
             '-v', '/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro',
             '-v', gocache + ':' + gocache,
             '-e', 'GOROOT=' + goroot, '-e', 'GOPATH=/home/aqyfzx/go',
             '-e', 'GOCACHE=' + gocache, '-e', 'GOPROXY=off', '-e', 'GOSUMDB=off',
             '-e', 'GOMAXPROCS=2', '-e', 'GOTOOLCHAIN=local', '-e', 'CGO_ENABLED=1',
             '-e', 'CGO_CFLAGS=-O2 -g -I' + str(headers),
             '-w', str(source), 'knowhub-build/golang:1.26-bookworm-ustc',
             goroot + '/bin/go', 'build', '-tags', 'sqlite_fts5', '-p', '2',
             '-o', str(base / 'WeKnora'), './cmd/server']
run('app-build', app_build)
run('race', [go, 'test', '-race', '-tags', 'sqlite_fts5', '-p', '2', '-count=1',
             './internal/mcpserver', './internal/middleware', './internal/logger',
             '-run', 'TestPersonalOutput|TestPersonalRegistry|TestPersonalProtocol|TestPrivatePayload|TestPersonalMCPLogger|TestV1NextEight|TestPortalM6PersonalDefaultClosed'])
run('vet', [go, 'vet', '-tags', 'sqlite_fts5', '-p', '2', './internal/mcpserver',
            './internal/middleware', './internal/logger', './internal/router'])
(results / 'build-done.json').write_text(json.dumps({'done': True, 'all_pass': all(x['exit_code'] == 0 for x in rows)}))
