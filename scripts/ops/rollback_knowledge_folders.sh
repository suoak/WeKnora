#!/usr/bin/env bash
# Code rollback only. Keep schema 3052 and all explicit folder rows.
# Run on the production host ONLY after the separate release/rollback approval.
set -euo pipefail
umask 077

if [[ "${1:---dry-run}" == "--dry-run" ]]; then
  printf '%s\n' 'Plan: validate original images and runtime snapshot; drain embedded Worker; back up current DB and folders; restore original APP/Frontend; keep schema 3052.'
  exit 0
fi
[[ "${1:-}" == "--execute" ]] || exit 2
release_dir="${2:?private release directory required}"

python3 - "$release_dir" <<'PY'
import json, os, pathlib, subprocess, sys
p = pathlib.Path(sys.argv[1])
assert not p.is_symlink() and p.resolve().parent == pathlib.Path('/home/aqyfzx')
assert p.name.startswith('knowhub-folders-release-') and p.is_dir()
assert p.stat().st_mode & 0o077 == 0, 'private directory must be mode 0700'
release = json.loads((p / 'release-provenance.json').read_text())
assert os.environ.get('CONFIRMED_ROLLBACK_RELEASE_SHA') == release['paired_source_sha'], 'explicit approved release identity required'
baseline = json.loads((p / 'production-baseline.json').read_text())
compose = json.loads((p / 'rollback-compose.json').read_text())
assert set(compose['services']) == {'app', 'frontend'}
for service in ['app', 'frontend']:
    expected = baseline['services'][service]['image_id']
    assert compose['services'][service]['image'] == expected
    subprocess.run(['docker', 'image', 'inspect', expected], check=True, stdout=subprocess.DEVNULL)
    name = 'WeKnora-' + service
    current = json.loads(subprocess.check_output(['docker', 'inspect', name]))[0]
    assert current['Image'] in {expected, release['images'][service]['image_config_digest']}, 'unexpected running image'
assert compose['services']['app']['environment']['AUTO_MIGRATE'] == 'false'
assert compose['services']['app']['environment']['DB_NAME'] == 'weknora', 'this path does not restore or switch databases'
assert any(m['target'] == '/app/jieba-dict' and m['read_only'] for m in compose['services']['app']['volumes'])
targets = {m['target'] for m in compose['services']['frontend']['volumes']}
assert {'/etc/nginx/conf.d/10-isolated-mcp-https.conf', '/etc/nginx/isolated-mcp-source-allowlist.conf', '/etc/knowhub/mcp-tls'} <= targets
ids = subprocess.check_output(['docker', 'ps', '-q']).decode().split()
containers = json.loads(subprocess.check_output(['docker', 'inspect', *ids]))
writers = []
for item in containers:
    env = dict(v.split('=', 1) for v in item['Config'].get('Env', []) if '=' in v)
    if env.get('DB_NAME') == 'weknora' and 'release-package_WeKnora-network' in item['NetworkSettings']['Networks']:
        writers.append(item['Name'])
assert set(writers) <= {'/WeKnora-app'}, 'additional writers require an updated coordinated rollback'
print('Original immutable images and protected mounts verified; no extra writer detected.')
PY

read_work_count() {
  docker exec -i WeKnora-postgres psql -X -U postgres -d weknora -qAt -v ON_ERROR_STOP=1 <<'SQL'
BEGIN READ ONLY;
SET LOCAL statement_timeout='10s';
SELECT
 (SELECT count(*) FROM feishu_legacy_attempts WHERE lease_until > now() AND status NOT IN ('published','obsolete')) +
 (SELECT count(*) FROM feishu_personal_attempts WHERE lease_until > now() AND status NOT IN ('indexed','obsolete','needs_attention')) +
 (SELECT count(*) FROM task_pending_ops WHERE claimed_at > now()-interval '10 minutes') +
 (SELECT count(*) FROM knowledges WHERE deleted_at IS NULL AND parse_status IN ('pending','processing'));
ROLLBACK;
SQL
}
[[ "$(read_work_count)" == "0" ]] || { printf '%s\n' 'Active work exists; do not force rollback.' >&2; exit 3; }
version=$(docker exec WeKnora-postgres psql -X -U postgres -d weknora -qAt -c 'SELECT version,dirty FROM schema_migrations;')
[[ "$version" == '3052|f' ]] || { printf '%s\n' 'Dirty or unexpected schema requires the separate database recovery plan.' >&2; exit 4; }

printf '%s\n' 'Gracefully stopping APP and its embedded Worker; no forced kill.'
docker stop --time -1 WeKnora-app
[[ "$(read_work_count)" == "0" ]] || { printf '%s\n' 'Leases remain; keep the maintenance window and review them.' >&2; exit 5; }
docker stop --time -1 WeKnora-frontend
stamp=$(date -u +%Y%m%dT%H%M%SZ)
full_backup="$release_dir/rollback-current-$stamp.dump"
folder_backup="$release_dir/rollback-folders-$stamp.dump"
set -o noclobber
docker exec WeKnora-postgres pg_dump -U postgres -d weknora -Fc --no-owner --no-acl > "$full_backup"
docker exec WeKnora-postgres pg_dump -U postgres -d weknora -Fc --no-owner --no-acl --table=public.knowledge_folders > "$folder_backup"
set +o noclobber
test -s "$full_backup" && test -s "$folder_backup"
sha256sum "$full_backup" "$folder_backup"
printf '%s\n' 'Starting the exact original compatible images; retaining schema and directory data.'
docker compose -p release-package -f "$release_dir/rollback-compose.json" up -d --no-deps --no-build --pull never app frontend
for attempt in $(seq 1 30); do
  if docker exec WeKnora-app curl --fail --silent --max-time 2 http://127.0.0.1:8080/health >/dev/null; then
    printf '%s\n' 'APP health passed. Independently verify authentication, retrieval, isolated MCP, and existing Feishu admission before ending maintenance.'
    exit 0
  fi
  sleep 2
done
printf '%s\n' 'APP health not confirmed. Backups and schema are retained; no stop lock was cleared.' >&2
exit 6
