# Reconcile the shared datasource queue

The normal signed steady-source policy may include `release_shared_queue: true`
in `POST /api/v1/datasource/:id/safety-recovery/steady/plan`.
This is an independently signed bounded reconciliation, not an unconditional
queue resume. Keep the currently approved source list unchanged unless each
additional source independently passes its management and provider checks.

Before activation the queue must be paused and quiescent, with at most 64
pending datasource-sync deliveries. Every delivery is bound to its exact Redis
message SHA256, tenant/source/run identity and current SQL outcome. Terminal
runs may only be acknowledged by the original consumer. The only nonterminal
release currently supported is a first-attempt, unclaimed GitLab run on a live,
active source and knowledge base. Unknown/live Feishu deliveries reject the
whole plan. Original SQL Claim remains authoritative at execution time.

Review the returned `shared_tasks` dispositions and current queue digest before
signing the exact plan with the existing offline Ed25519 key. Use the normal
Web operator API and existing `steady/activate` operation. Actor, source
management authority, worker incarnation, configuration, SQL proof, permit,
audit and Redis WATCH/Lua CAS are rechecked. A stale plan must be regenerated,
never retried with changed evidence.

Activation stores an immutable byte-exact checkpoint under the protected
Safety namespace, appends signed audit, installs version 3 policy, and removes
the shared pause last. No delivery is deleted, copied or re-enqueued. The
original consumer acknowledges terminal runs without Claim and consumes valid
GitLab jobs under their original identities. Approved Feishu work continues to
use `approved_sync`; unapproved/hold/revoked sources are not admitted.

After normal consumption, policy validation uses the signed immutable incident
checkpoint rather than treating new queue activity as tampering. Short health
leases, source checks and global trip remain in effect. A new trip must bind a
new physical queue snapshot and lifecycle. Missing/tampered checkpoint, renewed
pause, expired health or old reader fails closed. Keep checkpoint and audit
with the incident evidence; do not delete either to recover service.

Verify all original task outcomes, SQL generations, source permissions, normal
scheduled enqueue, and real changed-document publication/retrieval. A no-change
SyncLog is not proof of changed-document acceptance. Rollback requires fencing
executions first: a version-2 reader cannot consume a version-3 policy safely.
Do not restore an old database snapshot over new business writes.
