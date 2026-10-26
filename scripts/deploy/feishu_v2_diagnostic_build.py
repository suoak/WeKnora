"""Compile only the isolated operator diagnostic using a verified stopped cache.

Does not build/deploy an application image or connect to any database/service.
The parent helper validates source archives, cache ownership and the pinned image.
"""
import sys

import feishu_v2_build_candidate as build

build.BUILD_SCRIPT = r'''#!/bin/bash
set -euo pipefail
export GOPATH=/cache/go
export GOCACHE=/cache/go-build
export GOROOT=/cache/go-sdk
export PATH=/cache/go-sdk/bin:$PATH
export GOTOOLCHAIN=local
export GOENV=off
export GOMAXPROCS=2
export CPATH=/cache/sqlite-headers
cd /workspace
test "$(go version)" = 'go version go1.26.9 linux/amd64'
go version > candidate-go-version.txt
go mod verify
go build -p 2 -o feishu-diagnostic ./cmd/feishu-diagnostic
sha256sum feishu-diagnostic > candidate-diagnostic.sha256
go version -m feishu-diagnostic > candidate-diagnostic-build-info.txt
'''

if __name__ == '__main__':
    if '--verify-first-consent' in sys.argv:
        sys.argv.remove('--verify-first-consent')
        verification = r'''
go test -p 2 -tags sqlite_fts5 -json ./internal/application/service -run '^TestFeishuV2OAuthRequestsCompleteReadOnlyImportScopes$' -count=1 -timeout=3m > candidate-first-consent-tests.jsonl
python3 - <<'PY'
import json,pathlib
rows=[json.loads(line) for line in pathlib.Path('candidate-first-consent-tests.jsonl').read_text().splitlines()]
passed=[r for r in rows if r.get('Action')=='pass' and r.get('Test')=='TestFeishuV2OAuthRequestsCompleteReadOnlyImportScopes']
assert len(passed)==1 and not any(r.get('Action')=='fail' for r in rows)
pathlib.Path('candidate-first-consent-test-summary.json').write_text(json.dumps({'status':'PASS','test_count':1,'test':'TestFeishuV2OAuthRequestsCompleteReadOnlyImportScopes','elapsed_seconds':passed[0]['Elapsed']}))
PY
'''
        build.BUILD_SCRIPT = build.BUILD_SCRIPT.replace('\ngo build ', '\n' + verification + '\ngo build ', 1)
    raise SystemExit(build.main())
