# Migration Governance Map

This document governs the KnowHub fork's PostgreSQL (`migrations/versioned`)
and SQLite (`migrations/sqlite`) histories. The machine-readable source of
truth is [`migrations/migration-map.json`](migrations/migration-map.json).

## Phase 1 support boundary

M0 gates these upgrade paths:

1. a fresh database to the current latest schema; and
2. an existing KnowHub database to the current latest schema.

Adopting a database whose migration history was created by upstream is
explicitly deferred. Its lineage and proposed renumbering remain recorded so
future work cannot accidentally reuse those numbers, but it is not a Phase 1
release gate.

## Runner capability and ordering

The application uses `golang-migrate` file sources. Runtime order is the
numeric prefix in each migration filename, not the manifest. M0 verifies with
the real SQLite driver that sparse high versions `001000` and `003000` execute
in numeric order. The runner's version type is `uint`, so these values are
supported.

The manifest is authoritative for validation, provenance, namespace ownership,
immutable checksums, and CI expectations. It does **not** currently drive the
runtime loader. Therefore a future migration must satisfy both conditions:

- its numeric prefix puts it in the intended runtime order; and
- its namespace and current-latest metadata are registered in the manifest.

No `001xxx`, `002xxx`, or `003xxx` production migration should be created until
this governance contract is passing in CI. Making runtime ordering directly
manifest-driven would require a custom source driver and is not part of M0.

## Numbering rules

| Range | Owner | Rule |
|---|---|---|
| `000000–000999` | Historical | Frozen. Never renumber, edit, or replace an applied migration. |
| `001000–001999` | KnowHub reconciliation | Lineage bootstrap and reconciliation only. |
| `002000–002999` | Upstream ports | Reviewed ports only; preserve upstream provenance in the manifest. |
| `003000–899999` | KnowHub product | New KnowHub migrations after governance adoption. |
| `900000–999999` | Emergency/recovery | Reserved for exceptional recovery; never use for routine product work. |

Numbers may be sparse. Every migration still requires a matching `.up.sql` and
`.down.sql` file with the same basename. PostgreSQL and SQLite migrations use
the same logical feature identity but do not need the same physical number.

## Current inventories and conflicts

KnowHub's frozen historical PostgreSQL sequence ends at `000096`:

| Version | Logical change |
|---|---|
| 91 | user-owned multi-space MCP API keys |
| 92 | MCP tool enabled flag |
| 93 | Knowledge Portal |
| 94 | MCP metadata |
| 95 | usage analytics |
| 96 | usage analytics phase 2 |

KnowHub's frozen historical SQLite sequence ends at `000017`:

| Version | Logical change |
|---|---|
| 13 | user-owned multi-space MCP API keys |
| 14 | MCP tool enabled flag |
| 15 | Knowledge Portal |
| 16 | usage analytics |
| 17 | usage analytics phase 2 |

Governed product migrations begin after that frozen history. The current
latest version in both dialects is `003000`, `user_mcp_credential_lifecycle`,
which adds only `client_type` and `token_hint` to `tenant_api_keys`.

Upstream PostgreSQL versions 91–106 and SQLite versions 13–25 overlap or pass
through KnowHub's occupied ranges. All of them are treated as lineage conflicts,
even when a raw number is not yet present in one KnowHub directory: raw upstream
numbers must never be copied into this fork.

The detailed upstream inventory is in the JSON manifest. Every entry pins the
source repository, source commit, source migration number, canonical migration
number, dialect, the Git blob IDs of its up/down files, and its adoption
strategy. The reviewed upstream snapshot is commit
`2a6a9c251734d12bf65bf17d8f3412689ebed4e1` from
`https://github.com/Tencent/WeKnora.git`. The mapping policy is:

- upstream PostgreSQL 91/92 are reviewed as equivalents of KnowHub 92/94;
- upstream PostgreSQL 93–106 reserve `002093–002106`;
- upstream SQLite 13 is reviewed as equivalent to KnowHub 14; and
- upstream SQLite 14–25 reserve `002014–002025`.

Existing KnowHub fork migrations retain their original installed numbers. Their
`001xxx` values are governance identities only, not replacement SQL files.

## Immutability and checksums

The manifest pins an aggregate SHA-256 for every `.up.sql` and `.down.sql` file
through PostgreSQL 96 and SQLite 17. To make the checksum independent of Git's
checkout settings, each file's CRLF line endings are normalized to LF before
hashing. The aggregate is computed from files sorted by basename, appending for
each file:

```text
<basename>\n<sha256-of-LF-normalized-file-bytes>\n
```

CI fails if a historical file is edited, removed, renamed, duplicated, or loses
its up/down pair. Fixes to an applied schema must be forward-only migrations;
the checksum must not be “updated to make CI green.”

## PostgreSQL / SQLite logical parity

`logical_parity` in the manifest maps each KnowHub feature to both dialects.
Where a standalone SQLite delta is intentionally absent (currently MCP metadata),
the manifest records `null` plus a reason rather than inventing false parity.
New cross-dialect product schema must register both migrations or document an
explicit non-applicability reason.

## CI gates

M0 provides these gates:

- manifest schema, namespace, inventory, pairing, and checksum validation;
- real-runner proof for sparse high version numbers;
- SQLite fresh install and KnowHub-baseline upgrade to latest;
- SQLite normalized schema fingerprint equality between those paths;
- SQLite sentinel-row preservation;
- PostgreSQL fresh install and KnowHub-baseline upgrade in CI;
- PostgreSQL schema-only dump fingerprint equality and sentinel preservation.

The M1 release gate upgrades the latest frozen KnowHub histories: PostgreSQL
version 96 and SQLite version 17. Earlier installations traverse the same
immutable sequence before applying governed product migrations.

## Recovery and rollback

Before an upgrade, take a database-native backup and record the current
migration version and schema fingerprint. If validation fails, stop application
traffic and restore the backup. Down migrations are development verification,
not the preferred production recovery mechanism, because irreversible data
transforms may exist. Never force a migration version until the database schema
has been inspected and reconciled with that version.

For a dirty migration, preserve logs and a schema dump, determine whether its
transaction committed partially, then either complete/reconcile it or restore
the backup. Sentinel failures or fingerprint divergence block release and must
not be waived by rewriting historical checksums.

## Deferred upstream database adoption

An upstream-created database cannot be identified safely from a version number
alone because the histories collide. A future adoption tool must fingerprint
objects and columns, classify the lineage, record reconciliation state, and
only then apply mapped `002xxx` ports. Until that exists, Existing Upstream DB →
KnowHub is documented but unsupported and is not a Phase 1 release gate.
