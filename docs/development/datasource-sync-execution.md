# Datasource sync execution ownership

Admission and execution are separate decisions, both owned by the database.
Manual and scheduled syncs create a single non-terminal `sync_logs` row under
the datasource lock. Public `running` includes queued work and retry wait.

Every successful worker claim increments `execution_generation` and sets
`execution_claimed=true`. The fence is `(sync_log_id, generation, claimed)`;
queue task IDs, retry counts and timestamps are not fence tokens. Duplicate
deliveries do not call connectors or publish results.

Checkpoint and completion use one short transaction: datasource lock, log CAS,
log/result update, datasource cursor/state update, commit. No HTTP, export,
document parsing or model call runs inside that transaction. A failed fence
publishes neither record. Concurrent user pauses are preserved.

Infrastructure retry releases only its owned generation while keeping the
logical run active. Its next claim obtains a new generation. Connector failures
that have exhausted request-layer retries terminate the run; the queue does not
replay an entire Feishu workflow to add another API retry layer.

Heartbeats and checkpoints refresh liveness. Startup recovery and admission
reuse the 30-minute stale policy; recovery invalidates the old generation.
An expired queue claim can be reacquired with a new generation. Late workers
cannot checkpoint or finalize. Recovery may fence a physically paused worker;
only the new generation remains an authorized execution. Fencing here covers
sync-log and datasource metadata, not a rollback of already ingested documents
or already sent third-party requests.

## Upgrade and diagnostics

Stop/drain all pre-fencing workers before migration and do not mix old and new
binaries. PostgreSQL upgrade adds migration 3023; SQLite adds 3021. The forward
migration terminates pre-upgrade active logs. Existing queue deliveries for
those terminal logs safely no-op; operators can start a new sync after upgrade.

New protocol logs contain only datasource/run IDs, phase and category. Never
log credentials, authorization headers or request bodies. Shared Feishu request
rate limits remain process-scoped; datasource single-flight is database-scoped.
TLS-aware fallback and SSRF transport are unchanged.

The dedicated Actions workflow tests disposable PostgreSQL and SQLite databases,
concurrency x50/x100, recovery and race. It is validation only: it publishes no
image, package, tag or release. Production database credentials are never used.
