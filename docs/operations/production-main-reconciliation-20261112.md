# Reconcile deployed KnowHub changes with main

## Scope and provenance

Production APP source `dc3f11db077a2eb6aef8abf1b8e3616d3f7a3377`
descends from production business baseline
`718c443c21085e4d30308b86563219b274843603`. That history contained 41 commits
absent from main at `a33502ed5b6c8074ae4d38a3cb4940f4ecfedc78`.
This merge retains their ancestry and main's existing commits, including the
subsequent MCP environment selector and help-menu release (PR #132).

The production history brings in document-local deletion and retained local
snapshots; independent workspace imports and retryable source synchronization;
bounded Feishu image recovery, spreadsheet layout and transport fixes; safe
knowledge settings during parsing; and document-scoped Wiki read, selection and
durable startup recovery. It includes existing regression tests and operation
records. Main's newer usage accounting and MCP isolation are retained.

`internal/mcpserver/personal.go` and its protocol guard test keep main's version:
the production differences there were the adaptation of the same isolation
patch to an older source baseline. Replacing main's files with those older
copies would remove newer accounting. Both routes remain fixed to eight read
tools, sharing the existing identity and knowledge authorization service.

Current non-secret APP/frontend/HTTP80/HTTPS8443 configuration and provenance are
archived in [the production snapshot](../../deploy/knowhub/production-20261112/README.md).
Formal keys, credentials and secret Compose/configuration contents are excluded.
OIDC dual-origin development and website HTTPS443 remain deferred; this merge
does not activate or deploy them.

## Two different deployed 3046 migrations

Main's 3046 is `mcp_usage_coverage`; the production branch's 3046 was
`feishu_local_deletion`. A numeric migration version alone cannot establish
schema equivalence. Keeping both active files under the same prefix fails the
runner's uniqueness contract. Simply choosing one file would silently skip the
other branch's schema on an existing 3046 database.

The canonical source retains main's 3046 SQL unchanged. The exact production
3046 PostgreSQL body is preserved as a test fixture, outside the active source.
New **3047 production_3046_reconciliation** converges both historical lineages:

- PostgreSQL reapplies the idempotent Feishu deletion state constraint and write
  fence, adds missing usage columns and resource-link scope support, and retains
  existing statistics and the rollout timestamp. It never overwrites current
  metadata from stale rollback rows.
- SQLite also accepts the production branch's former no-op 3046. It rebuilds
  the two usage tables into the canonical shape, preserving event/resource IDs,
  all existing fields, metadata and AUTOINCREMENT high-water marks. Existing
  columns take precedence over fallback aliases when they already exist.
- A one-step 3047 down migration intentionally retains the additive schema and
  write fences: its ambiguous 3046 predecessor is not sufficient evidence to
  remove either branch's data or safety behavior. Up can run again safely.

Regression tests start from both genuine 3046 chains through golang-migrate,
without Force or editing version rows. They verify preservation, clean 3047,
repeat down/up, and the PostgreSQL deletion fence. Existing latest-version
expectations and the governance manifest advance together.

## Deployment boundary

This is source/configuration reconciliation only. Production remains on schema
3046 and the current images; no service, Key, authorization, Feishu lock/queue,
certificate, network or database is changed by this merge.

A future APP built from main must be separately approved and tested as a release
that includes **3047**, not represented as the earlier no-migration isolation
candidate. Keep automatic migration disabled on the current production release.
Apply the reviewed migration only in an approved database change procedure with
the current recovery protections and verified lineage; do not Force a version.
