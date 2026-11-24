# Published Feishu derived writeback (schema 3051)

A published legacy-managed Feishu document can regenerate its description,
structured profile and derived metadata without reopening the source publication
lease. The worker binds the queued job to the resource generation, published
attempt and document incarnation. Historical jobs without that binding are
ignored; a new explicit summary request uses the current published identity.

Every write revalidates the existing source owner's live authorization and the
caller's inherited read guard. A transaction locks the source, resource and
knowledge row, checks the published attempt and document epoch/digest, then issues
and consumes a single-use grant in that same transaction. The trigger permits
only description, profile, summary_status, metadata.derived and updated_at.
Neither this grant nor the summary worker can replace source chunks, vectors,
content, permissions, source identity or publication receipts. Competing snapshots
cannot overwrite the winner. Revocation and superseded publications stop the
job; ordinary PostgreSQL serialization errors retain normal bounded retries.

## Migration

3051 extends the current 3048 publication trigger. It does not adopt the historical
3043 proposal or edit an applied migration. Old workers retain their original
write fence. Rollback restores the complete 3048 trigger, including folder-only
classification and local deletion, before dropping the new grant table. Existing
documents, attempts, receipts and authorization rows are never backfilled.
SQLite records the same version as a no-op; protected legacy source publication
remains PostgreSQL-only.

`TestKnowledgeDerivedWritePostgres` uses disposable databases and the real runner
to verify 3050 -> 3051, 3051 -> 3050, and re-upgrade, including write fencing after
rollback. The CI gate also runs summary/publication race regressions and real
SQLite upgrades. No skipped check is evidence of successful live functionality.

## Explicit live acceptance

`TestFeishuDerivedCurrentProtocolLive` requires the `feishu_live_acceptance` build
tag and explicit environment opt-in. It clones only the designated tenant 10000
test document, its publication dependencies and current authorization records
into disposable databases. Snapshots must remain private, outside this repository.
It verifies legal metadata updates, rejected content writes, source/connection/
member revocation, publication generation changes, missing-model completion and
actual model-generated summaries. Published chunks and vectors must retain their
full row digests throughout. Token refresh is prohibited in the clone.

An optional `FEISHU_DERIVED_MODEL_TUNNEL` can route the existing model connection
over an authenticated, exact-target SSH tunnel when the test host cannot reach
the model subnet. The original URL, unchanged SSRF policy and safety transport
remain active; connections to another target are rejected. This fixture never
writes to the production database and never deploys an application.
