# Wiki claim recovery during worker shutdown

Asynq may retire a worker after its shutdown deadline while an adapter still waits
for a model response. Cleanup deferred until that adapter returns cannot guarantee
that SQL pending-operation claims are released at worker retirement. A renewable
generation lease intentionally survives request cancellation for some historical
operations; worker shutdown now explicitly relinquishes its own lease.

The ingestion lifecycle registers cancellation cleanup immediately after claiming
SQL rows. Cancellation and normal/early return use the same bounded, idempotent
release. It compares the persisted claim timestamps, tenant, knowledge base, scope
and operation type. It retains the pending rows, payloads and failure counts.

For an activated document-selection Wiki, the next ingestion/finalization worker
can recover prior claims after acquiring the existing exclusive KB generation
lease. Recovery checks the actual opaque Redis token before the transaction and
after locking the SQL snapshot. It handles at most 1,000 rows per generation and
records a metadata-only `wiki_claim_recovery_audit` receipt. Missing leases, changed
tokens, wrong tenants and inactive spaces cannot authorize recovery.

Claim-bound delete, release and failure-count updates compare the original claim
timestamps. A late handler cannot settle a replacement worker's rows. Cancellation
also invalidates the worker's detached publication authority before releasing the
Redis token. Other renewable-lock consumers retain their existing cancellation
semantics.

## Release checks

Run the Wiki recovery CI gate, including `TestWikiClaimAsynqShutdownPostgres` with
an explicitly disposable PostgreSQL DSN. This test runs the real Asynq worker with
a deliberately cancellation-ignoring adapter and verifies metadata recovery,
lease handoff and fencing of late settlement before the adapter returns.

During production handoff, distinguish claims owned by the retiring task from
claims that already existed before deployment. A knowledge-base-wide nonzero
claim count alone does not establish that the retiring handler leaked its claims.
Do not erase claims through operator SQL or skip ownership checks to make a
deployment gate pass. After deployment verify recovery audit receipts, fresh
claim timestamps, advancing Wiki output and resumed application-owned queues.
Datasource synchronization must remain enabled.

These changes need no schema migration and do not clear historical regeneration
blocks. Those blocks require their existing verified regeneration protocol.
