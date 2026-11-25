# Single-source observation candidate

This candidate does not authorize production recovery. No source status,
cursor, archived task, permit or historical trip may be edited manually.

## Authorization sequence

1. Record the actual namespace, incident, live worker identity, membership,
   source authority, generation/execution fences, restore evidence and rollback.
2. With explicit administrator approval, use the EXISTING normal-service
   deactivation operation to durably revoke the installation-wide grant. It
   preserves SQL history and closes execution. Never emulate it with SQL/Redis.
   This is deliberately a separate restrictive prerequisite, not a side effect
   of a read-only plan. An older binary also rejects this inactive grant.
3. Submit the existing prepare request with `hold_for_review: true`. It rejects
   an active wide grant, unknown/mismatched SQL/Redis authorization, nonempty
   dedicated pending lane, live work, unauthorized source, or changed incident.
   The shared queue and dedicated archived tasks are retained.
   The existing offline CLI uses `--hold-for-review --expected-pending 0` for
   plan/prepare/approve/execute. It rejects implicit changes of observation
   intent, a missing inactive-grant digest or a nonempty dedicated checkpoint.
4. Review the returned `hold_for_review`, inactive `normal_service_sha256`,
   incident, namespace, current SHA, worker/incarnation, queue and source facts.
   Use the existing independent Ed25519 reset and canary approvals and expiry.
   No private key belongs in the application or repository.
5. The canary runs only the selected source. Success leaves the dedicated lane
   paused and ordinary source admission closed. Inspect originals, chunks,
   embeddings, permissions, index and publication BEFORE further approval.
6. Obtain a NEW independent `steady_sources` signature for an explicit source
   list, actor, authority, revision and validity. It binds the observation plan,
   rechecks live authority and archive-terminal proof, and only then releases the
   dedicated lane. It does not reactivate the wide grant. Subsequent changes of
   scope require another signed replacement policy. No shared-queue release or
   legacy continuity shortcut is accepted during observation.

Any missing/mismatched observation marker, recovery state, inactive grant,
signature, health proof, identity or authority fails closed. Consumer health
cannot silently restore broader authority. A binary rollback retains the durable
revocation and may require a new reviewed recovery; it must never restore the old
active grant or reuse old permits. Deactivation failure/ambiguous acknowledgement
requires reconciliation of the existing durable records, never an ad hoc write.

## Verification status

Windows native compilation encountered a MinGW `__stack_chk_fail` link failure;
no CGO or safety mechanism was disabled. Linux real-Redis/race and PostgreSQL
regression results are pending for this candidate. Component fixtures use
disposable signing identities, not legitimate production recovery approvals.
Full APP authorized-source acceptance and production recovery are NOT_RUN.

The current minimal observation path requires an empty dedicated pending lane;
it never clears pending or archived tasks to meet that requirement. Nonempty
historical work needs a separately reviewed disposition before recovery.
