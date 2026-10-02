#!/usr/bin/env bash
# Validation only: derive latest from the actual source files, then compare the
# complete source migration tree with the built image. Never hardcode latest.
set -euo pipefail
image=${1:?usage: check-app-migration-identity.sh IMAGE}

latest_from_files() {
  local dialect=$1 directory latest declared
  directory=$(jq -er ".dialects.${dialect}.directory" migrations/migration-map.json) || return 1
  latest=$(find "$directory" -maxdepth 1 -type f -name '*.up.sql' -printf '%f\n' |
    awk -F_ '$1 ~ /^[0-9]+$/ {print $1 + 0}' | sort -n | tail -1) || return 1
  declared=$(jq -er ".dialects.${dialect}.current_latest" migrations/migration-map.json) || return 1
  if [ -z "$latest" ] || [ "$latest" != "$declared" ]; then
    echo "Migration source/manifest latest mismatch for $dialect: files=$latest manifest=$declared" >&2
    return 1
  fi
  printf '%s\n' "$latest"
}

expected_versioned=$(latest_from_files versioned)
expected_sqlite=$(latest_from_files sqlite)
if [ "$image" = --source-only ]; then
  echo "EXPECTED_VERSIONED_LATEST=$expected_versioned"
  echo "EXPECTED_SQLITE_LATEST=$expected_sqlite"
  exit 0
fi
audit_tmp=$(mktemp -d /tmp/knowhub-migration.XXXXXXXX)
[[ "$audit_tmp" == /tmp/knowhub-migration.* ]] || exit 1
container=
cleanup() {
  if [ -n "$container" ]; then docker rm "$container" >/dev/null; fi
  rm -rf -- "$audit_tmp"
}
trap cleanup EXIT
container=$(docker create "$image")
docker cp "$container:/app/migrations" "$audit_tmp/migrations"
(cd migrations && find . -type f -print0 | sort -z | xargs -0 sha256sum) > "$audit_tmp/source.sha256"
(cd "$audit_tmp/migrations" && find . -type f -print0 | sort -z | xargs -0 sha256sum) > "$audit_tmp/image.sha256"
diff -u "$audit_tmp/source.sha256" "$audit_tmp/image.sha256"

echo "EXPECTED_VERSIONED_LATEST=$expected_versioned"
echo "EXPECTED_SQLITE_LATEST=$expected_sqlite"
echo 'SOURCE_IMAGE_MIGRATION_IDENTITY_GATE=PASS'
if [ -n "${GITHUB_ENV:-}" ]; then
  printf 'EXPECTED_VERSIONED_LATEST=%s\nEXPECTED_SQLITE_LATEST=%s\n' \
    "$expected_versioned" "$expected_sqlite" >> "$GITHUB_ENV"
fi
