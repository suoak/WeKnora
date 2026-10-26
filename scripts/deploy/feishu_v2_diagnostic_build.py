"""Compile only the isolated operator diagnostic using a verified stopped cache.

Does not build/deploy an application image or connect to any database/service.
The parent helper validates source archives, cache ownership and the pinned image.
"""
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
    raise SystemExit(build.main())
