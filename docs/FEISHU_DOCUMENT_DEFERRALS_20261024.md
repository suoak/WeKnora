# Exact-document authorization deferrals

The shared Wiki/Drive streaming engine previously stopped a source when a single
document failed content admission. That behavior remains the default for unknown
errors. Explicit, source-owned document policies now allow an administrator to
defer named resource identities while the remaining source continues.

## Data and authorization

Migration 003038 adds `data_sources.document_deferrals`, an empty JSONB array by
default. Connector configuration, permissions, accepted revision, generation,
leases, and existing knowledge are not rewritten. Generic datasource updates
cannot replace the policy. Policies require exact tenant/source/KB/root/document
identities, reason `awaiting_document_owner_authorization`, and an audit reference.
Titles are descriptive, never identity selectors. The server records the real
authenticated administrator and time; another tenant's borrowed execution context
cannot authorize a policy edit.

Admin-only GET/PUT `/api/v1/datasource/:id/document-deferrals` expose the policy and
its canonical hash. Replacement requires the current hash, a non-running source,
and active/error eligibility. Paused, hold, revoked and deleted sources reject
changes. Updates use the same datasource-first database lock as execution claims.
The activity audit records old/new policy hashes. Prior sync logs retain the full
reason, identity, author, time and reference after a policy is removed.

## Durable resumption

Before listing or fetching, active policies are checkpointed as pending entries in
`connector_cursor.feishu_pending_documents_v1`. Keys are SHA256 hex strings so
they survive PostgreSQL JSONB; the exact identities remain in values. A failed
checkpoint aborts before source requests. Specified documents are skipped before
content fetch and timestamp shortcuts, including overlapping selected roots.
Their old accepted timestamps and published knowledge remain intact. They are
also excluded from deletion handling while the policy applies.

Removing a policy retains its pending marker. On subsequent sync, pending
identities bypass the unchanged-timestamp shortcut. The marker clears only after
all fetched items are admitted, or an ordinary confirmed deletion is accepted.
Normal version ownership, permission checks, atomic publication and execution
fencing still apply. A failed retry remains pending. The sync log provides an
administrator-only authorization-retry action using policy hash replacement and
the existing sync trigger; no cursor reset or source resume is performed.

## Results

Logs distinguish `success`, `partial`, `deferred`, `failed`, and `canceled`.
Deferred policy count and records, discovered, unchanged and unsupported counts
are separate from actual created/updated/deleted/skipped/failed item counts.
An active policy prevents full-success or full-reconciliation claims, including
when the document is absent from the listing. Only full success advances the
last-success timestamp. Partial and deferred sources retain automatic scheduling;
whole-source pause/access restrictions still take precedence. Unsupported native
formats remain visible as unsupported on unchanged runs. Unknown errors continue
to abort and surface normally.

## Validation and release

Focused tests cover exact identity validation, cross-tenant rejection, spoofed
authors, busy/protected sources, policy CAS contention, real PostgreSQL JSONB,
restart plus unchanged-timestamp retry, overlapping roots, deferred deletion
protection, checkpoint failure, unknown errors, and result classification.
The integrated CI workflow runs these with race and a real PostgreSQL instance,
alongside the existing atomicity, Redis, parser, BM25 and backend regression gates.
Frontend type checking, build and locale validation are required for this change.

Before activation, validate an isolated two-document real sync: publish both,
defer one, sync the other, restart, remove the policy, retry with unchanged source
time, and verify published content, parent graph, vectors and retrieval. Do not
classify this application validation as completed based on unit tests alone.

Production rollout must drain old workers and freeze sync, retain existing
database/files/config backups, apply and verify migration 003038, then start the
new exact-source APP and frontend with sync admission closed. Install only the
four user-specified identity policies through the authenticated API after checking
there are no running tasks. Compare configuration, ACL and protected-source
snapshots. Enable eligible sources through the existing controlled release flow,
then observe real terminal results and resource inventory before reporting counts.

Rollback retains the new policy column and pending cursor for audit and recovery.
The PostgreSQL down migration intentionally preserves policy data. SQLite
downgrade refuses active policies or pending retries before dropping an empty column. An old APP does not
understand deferrals, so it must remain sync-frozen during rollback; stopping old
workers and blocking outbound requests is required. Never resume old consumers
under an assumed document policy. No source or backup cleanup is authorized.
The original old-source cutoff remains 2026-10-27 19:25 Asia/Shanghai; any extension
requires approval and must not be applied automatically.
