# Feishu document failure isolation (2026-10-24)

Production baseline: df1fc531a21d973b4d4d11468e04ade4ef8e45bf (PRs #42/#43).

## Reproduced problem and bounded change

The four authorized deferrals work, but a later `source_revision_content_conflict`
still aborts `streamSyncHandler.Emit` and the shared Wiki/Drive engine. Export
range requests now finish the large customer document download; safe staging
subsequently refuses the document, and the old generic refusal hid its precise
parser/shape reason. No conclusion that the customer document contains parser
image references is claimed without a fresh structured refusal observation.

Only these recorded ingest failures may continue to another document:

- Known `feishusafety.ErrBudgetBusy` backpressure; denied requests stay denied,
  Redis/permit/guard failures remain fatal.
- Exact `LegacyFenceError.Reason == source_revision_content_conflict`.
- Typed safe staging shape refusals: versioned image staging required, audio,
  empty parsed content, or chunk count outside the supported atomic graph cap.

All such documents count as **failed**, never deferred or successful. Only the
four explicit admin policies count as deferred. Unknown parser errors, generic
staging unavailability, cancellation, execution/claim/generation/lease errors,
revision regressions, graph/parent errors and authorization uncertainty retain
their fatal behavior. No publish guard or source quarantine rule is disabled.

The engine saves `feishu_failed_documents_v1` with exact source-root/document
identity and stable error code in its existing fenced cursor checkpoint. It
restores the previous accepted edit time (or removes the unaccepted new entry),
checkpoints immediately, and continues. Pending failures override the unchanged
fast path after restart; acceptance removes them. Failure cursor snapshots are
independent of authorization deferrals and bounded to 1,000 identities. Failed
only runs report failed; mixed accepted/failed runs report partial. Neither is a
complete synchronization nor advances last-full-success metadata.

No schema migration, policy additions, permission changes, cursor resets,
watermark edits, generation edits, hash substitution or knowledge deletion.

## Validation

Isolated Linux race run v1: 410 JSON test events, exit 0. Includes known failure
continuation, cursor round-trip/restart, same-time retry, unaccepted-new-item
cursor protection, unknown/stale/canceled/checkpoint fences, existing document
deferral tests, export range tests, PostgreSQL policy fencing and migrations.
Further final-SHA full CI and real parser PostgreSQL assertions are required
before merging or production deployment of this incremental fix.

## Release and rollback

Use the already validated freeze/drain/image smoke flow. Database is already
3038; do not re-run a hardcoded 3037-to-3038 migration or downgrade. Preserve the
current app/frontend images, policies, pending failures and backups. Rollback
keeps 3038 and freezes Feishu egress/consumption until the restored worker is
verified. Existing df1 workers cannot interpret the new failure cursor: do not
resume old workers against it before a compatibility fix or a validated forward
repair. Paused/hold/revoked sources remain untouched.

Completeness must be measured from a fresh source catalog versus published
knowledge and actual Portal/MCP retrieval, not a zero-change or partial run.
Old sources/backups are retained; October 27 19:25 CST expiry requires owner
extension approval if completeness acceptance remains unfinished.
