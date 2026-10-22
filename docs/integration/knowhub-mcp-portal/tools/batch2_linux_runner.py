"""Isolated Linux test worker; run only in a new task directory on test host.

Never uses production containers, DBs, credentials, documents or queues.
Each command retains original stdout/stderr and exit code. No assertion skips.
"""
import datetime
import json
import os
from pathlib import Path
import subprocess
import sys

BASE = Path(__file__).resolve().parent
SOURCE = BASE / 'source'
LOGS = BASE / 'results'
LOGS.mkdir(exist_ok=True)
results = []


def run(name, args, env=None, cwd=SOURCE, timeout=2400):
    started = datetime.datetime.now(datetime.timezone.utc).isoformat()
    with (LOGS / (name + '.log')).open('wb') as output:
        try:
            p = subprocess.run(args, cwd=cwd, env=env, stdout=output, stderr=subprocess.STDOUT, timeout=timeout)
            code = p.returncode
        except subprocess.TimeoutExpired:
            code = 124
    results.append({'name': name, 'command': args, 'exit_code': code, 'started_utc': started,
                    'finished_utc': datetime.datetime.now(datetime.timezone.utc).isoformat()})
    (LOGS / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
    return code


env = dict(os.environ, GOTOOLCHAIN='local', GOSUMDB='sum.golang.org', CGO_ENABLED='1',
           GOCACHE=str(BASE / 'go-cache'))
# Existing module cache is managed by Go's atomic downloads; no business state.
run('go-version', ['go', 'version'], env)
run('go-full', ['go', 'test', '-count=1', './internal/application/access', './internal/types',
    './internal/application/repository', './internal/middleware', './internal/mcpserver',
    './internal/router', './internal/application/service', './internal/agent/tools'], env)
run('go-race', ['go', 'test', '-race', '-count=1', './internal/application/access',
    './internal/application/repository', './internal/middleware', './internal/mcpserver'], env)
run('go-vet', ['go', 'vet', './internal/application/access', './internal/application/repository',
    './internal/middleware', './internal/mcpserver', './internal/application/service', './internal/types'], env)
run('postgres-migrations', ['python3', str(SOURCE / 'docs/integration/knowhub-mcp-portal/tools/batch2_pg_migrations.py'), str(BASE)], env)
# Dependency installation is isolated in a disposable container; test execution
# itself has no network so no accidental calls to business services can occur.
run('python-deps', ['docker', 'run', '--rm', '-v', str(SOURCE)+':/source:ro', '-v',
    str(BASE / 'python-deps')+':/deps', 'python:3.11-slim', 'sh', '-c',
    'pip install --disable-pip-version-check --target /deps -r /source/mcp-server/requirements.txt'], env, timeout=600)
run('python-full', ['docker', 'run', '--rm', '--network', 'none', '-v', str(SOURCE)+':/source:ro', '-v',
    str(BASE / 'python-deps')+':/deps:ro', '-e', 'PYTHONPATH=/deps', '-e', 'PYTHONDONTWRITEBYTECODE=1',
    '-w', '/source', 'python:3.11-slim', 'python', '-m', 'unittest', 'discover', '-s', 'mcp-server', '-p', 'test_*.py', '-v'], env, timeout=600)
(LOGS / 'done.json').write_text(json.dumps({'completed': True, 'production_changes': False,
    'source_manifest': json.loads((BASE / 'manifest.json').read_text()), 'results': results}, indent=2) + '\n')
