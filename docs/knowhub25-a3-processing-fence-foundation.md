# A3 processing fence foundation

Baseline: `a6403708915680ccad310b0fd64506597b879804`.
Fixed upstream: `005627b180c6c7509660b0a14401e463f48d53f8`.
Sources: `95691daab2e292a8c45e0ae200737b1c9e5de222` (index retry) and
`cbfd66fd922df3632448e0bfd5925a6c10196fc8` (quota cleanup).

This batch is a repository foundation, **not an activated worker fix**. A3 remains
PARTIAL. It adds no migration execution, worker activation, production connection,
or historical queue conversion. The PostgreSQL 16 Bookworm test image is a pinned
nonproduction fixture; it does not establish production PostgreSQL/schema3023 identity.

## SQL invariants

Allocation, renewal, guarded chunk persistence and terminal updates take the same
tenant-scoped knowledge row lock. Generation and a fresh UUID execution capability
are persisted before use. Database time governs expiry. Expired or replaced
capabilities cannot revive through renewal, write chunks, or finish processing.
Cancel/delete checks occur while holding the lock, not before an unconditional Save.

Chunk rows and their exact ownership ledger are committed together. A failed chunk
write rolls back both. Cleanup uses tenant + knowledge + owned Chunk IDs. It never
deletes a knowledge's entire chunk set, including when an old execution has expired.
The ledger remains incomplete until every configured vector backend acknowledges
cleanup. A failed DOWN refuses to discard an incomplete ledger; UP is additive and
does not reset historical knowledge status, file metadata or subtask counters.

## Activation work still required

1. Unify all three production OpenAttempt paths (reparse, manual processing and
   legacy document payload fallback), plus initial queued and inline passage creation.
   Existing span MAX(attempt)+1 is a diagnostic trace number, not an execution lease.
2. Extend fixed task payloads with generation/nonce and distinguish user-requested
   new generation from queue redelivery. Asynq and Lite must share the same claim,
   heartbeat, retry and expiry protocol; retries must not silently allocate a new
   user generation. Legacy payload conversion must be explicit and reviewed.
3. Replace active processChunks writes and terminal transitions with guarded
   repository operations. Cancel/delete/source replacement and all counter writers
   must obey the same transaction boundary before activation.
4. Persist vector cleanup target identity (backend, collection/dimension/config
   generation) alongside ownership. Existing DeleteByChunkIDList implementations
   use chunk payload fields; SQL Chunk ID must not be substituted for hashed point ID.
   Verify all configured backends and tenant context separately.
5. Propagate cleanup failure. Retriable index failure must return an error while
   retry budget remains; only the matching final execution may settle failed.
   Keep already successful new-generation data, and never acknowledge stale cleanup
   as a completed user task.
6. Register migration provenance in the migration map and validate fresh install,
   schema3023 upgrade/down, historical payloads, rollback, crash recovery and bounded
   cleanup retries. Rollback requires drained workers and resolved cleanup worklists.

The foundation PG tests cover actual row locks, concurrent generation allocation,
exact-ID cleanup, rollback, lease invalidation and migration idempotence. They do not
claim an end-to-end worker, vector backend, or release Gate PASS.

## CI evidence boundary

The dedicated workflow uses Ubuntu 24.04, Go 1.26.7, fixed Action SHA and a fixed
official PostgreSQL image digest with synthetic service credentials. Raw Go output
stays in mode-0700 runner storage. No artifact upload is configured. Only fixed test
IDs, exit/status, original full-file hashes and rule/count metadata reach the console.
Credential-pattern hits, malformed event streams, missing/skipped necessary tests,
nonzero exits and Race reports block the batch. Product acceptance remains separate.

## Production runtime development revision

Document and manual generation-bearing tasks now enter a shared claim/renew/release
boundary. A successful main delivery is persisted as settled independently of
post-processing status; queue redelivery cannot restart a settled main worker.
Retryable failure releases the lease without allocating a user generation.
Supersession cancels the worker and guarded SQL still rejects its old capability.
Rollback additionally refuses live workers, even with completed cleanup ledgers.

This is not full activation: existing enqueue sites do not yet emit generation,
legacy payload conversion remains explicit work, and per-backend cleanup target
snapshots plus all counter/subtask guards must be wired before the admission
batch is enabled. No production migration has been run.
