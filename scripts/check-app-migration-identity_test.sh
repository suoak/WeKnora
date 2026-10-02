#!/usr/bin/env bash
set -euo pipefail
helper=$(realpath scripts/check-app-migration-identity.sh)
fixture=$(mktemp -d /tmp/knowhub-migration-test.XXXXXXXX)
[[ "$fixture" == /tmp/knowhub-migration-test.* ]] || exit 1
trap 'rm -rf -- "$fixture"' EXIT
mkdir -p "$fixture/migrations/versioned" "$fixture/migrations/sqlite"
cp migrations/migration-map.json "$fixture/migrations/migration-map.json"
versioned=$(jq -er '.dialects.versioned.current_latest' migrations/migration-map.json)
sqlite=$(jq -er '.dialects.sqlite.current_latest' migrations/migration-map.json)
touch "$fixture/migrations/versioned/${versioned}_fixture.up.sql"
touch "$fixture/migrations/sqlite/${sqlite}_fixture.up.sql"
(cd "$fixture" && bash "$helper" --source-only) | grep -Fx "EXPECTED_VERSIONED_LATEST=$versioned"

# Future versions must work without editing either workflow or helper.
next_versioned=$((versioned + 2))
next_sqlite=$((sqlite + 1))
touch "$fixture/migrations/versioned/${next_versioned}_fixture.up.sql"
touch "$fixture/migrations/sqlite/${next_sqlite}_fixture.up.sql"
jq --argjson pg "$next_versioned" --argjson sqlite "$next_sqlite" \
  '.dialects.versioned.current_latest=$pg | .dialects.sqlite.current_latest=$sqlite' \
  "$fixture/migrations/migration-map.json" > "$fixture/migrations/updated.json"
mv "$fixture/migrations/updated.json" "$fixture/migrations/migration-map.json"
actual=$(cd "$fixture" && bash "$helper" --source-only)
grep -Fx "EXPECTED_VERSIONED_LATEST=$next_versioned" <<< "$actual"
grep -Fx "EXPECTED_SQLITE_LATEST=$next_sqlite" <<< "$actual"

# A stale manifest must fail closed instead of silently accepting runtime drift.
jq --argjson old "$versioned" '.dialects.versioned.current_latest=$old' \
  "$fixture/migrations/migration-map.json" > "$fixture/migrations/updated.json"
mv "$fixture/migrations/updated.json" "$fixture/migrations/migration-map.json"
if (cd "$fixture" && bash "$helper" --source-only); then
  echo 'Stale migration manifest was incorrectly accepted'
  exit 1
fi
echo 'DYNAMIC_MIGRATION_ASSERTION_TEST_GATE=PASS'
