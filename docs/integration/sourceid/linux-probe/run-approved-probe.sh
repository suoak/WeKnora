#!/usr/bin/env bash
set -eu
set +x
ulimit -c 0
probe_dir=/home/aqyfzx/ai/sourceid-protocol-probe-a4fdb8052df1
runtime_dir=/home/aqyfzx/ai/sourceid-protocol-probe-c855338b9530
[ "$#" -eq 1 ] && [ -f "$1" ] || exit 2
exec env -i PATH=/usr/bin:/bin HOME="$probe_dir" JIEBA_DICT_DIR="$runtime_dir/dict" LOG_PATH= LOG_LEVEL=error LANGFUSE_ENABLED=false LLM_DEBUG_LOG=false TZ=UTC \
 "$runtime_dir/compat-runtime/usr/lib/x86_64-linux-gnu/ld-linux-x86-64.so.2" \
 --library-path "$runtime_dir/compat-runtime/usr/lib/x86_64-linux-gnu" \
 "$probe_dir/sourceid-probe" -metadata "$1" -listen 127.0.0.1:18183
