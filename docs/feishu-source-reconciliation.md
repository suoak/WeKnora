# Controlled source reconciliation

Normal manual and scheduled admission continues to reject held, revoked and
paused sources. An operator may propose an exact `reconcile_sources` list in
the steady policy API, independently of its normal `sources` list. Every space
and private KB management permission is checked for the real Web actor.

The offline signature binds each source, tenant, KB, configuration, authority,
incident lifecycle and a one-hour reconciliation deadline. This deadline is
separate from the normal service authorization. A reconciliation purpose does
not authorize ordinary manual or scheduled jobs.

`POST /datasource/:id/safety-recovery/reconcile` consumes one persistent ticket
and creates a normal SyncLog and full-scan task on `approved_sync`. The ticket
is bound to that exact SyncLog before enqueue. Unknown enqueue outcomes require
a new inspected and signed policy; the operation is not blindly retried.

After SQL Claim, the worker restores purpose only for the bound source, tenant,
run and actual generation. Guard, permit, source authority and publication
fences remain active. Purpose, authority and deadline are rechecked before
candidate lease/write operations, checkpoints and the final source transition;
terminal failure cleanup still closes its SQL Claim after approval expires.
The archived identity read proof is distinct from full
reconciliation completion: intermediate checkpoints cannot release hold.
Only the existing complete-scan, reconciled-publication transaction may perform
the formal source transition. Missing dependencies or any incomplete item leave
the source protected. Historical live rows attributed to the source must have
current-run receipts; untracked legacy Feishu rows in the KB block hold release.
Completed traversal alone is never used to erase unknown provenance. Revoked
and paused sources are never eligible.

After a successful source transition, obtain a fresh normal-service policy.
The old configuration/authority binding cannot silently promote reconciliation
into ongoing access. A failed source does not authorize a different source.

Policy replacement may preserve archived approved-lane tasks only when their
exact message bytes and SQL terminal, unclaimed SyncLogs are proven. Signed
archive count and digest, Redis WATCH and queue rechecks bind the proof through
activation. Pending, active, retry or scheduled work prevents replacement.
Unknown archives fail closed. No archived message is removed or replayed.
The original shared `sync` lane remains paused and its six tasks unchanged.

No migration is needed. New signed fields use `omitempty` for unchanged normal
certificates. After issuing a reconciliation certificate, do not roll back to a
reader that cannot validate it: first close admission, fence and drain executions,
then replace the policy using a compatible controller. Never restore an old
database snapshot over new publications.

Targeted coverage includes actual Asynq archives, actor/space isolation, one-shot
and concurrent ticket acquisition, forged payloads, grant/config/expiry/trip
rejection, SQL Claim finalization and PostgreSQL hold-preserving checkpoints.
