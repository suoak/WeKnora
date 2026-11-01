# Personal document retry: release boundaries

This change continues an unpublished, legitimately terminated personal OAuth
v3 first-create execution. It does not enroll documents in legacy datasource
sync, open Safety, change hold/revoked, or recover the shared sync queue.

## Admission and execution

- Normal Web requests must have live matching membership, the ordinary
  creator-or-Admin mutation role, canonical KB editor access, and an independent
  original source-owner grant. A claimed administrator role is insufficient.
- Source preflight checks live grant/ACL/revision, manifest and file hashes,
  unique resource binding, the original outbox, and the sealed execution.
- Admission writes a separate durable intent; the original operation and
  sealed resource/attempt remain unchanged. Claim repeats current authority
  under the normal resource/attempt locks and mints a new nonce/lease.
- The verified replacement claim and quarantine commit together. Only private,
  unpublished, disabled, unedited and unreferenced stage rows are eligible.
  Preserve the previous attempt, original outbox, receipt, chunks and vectors
  in an AES-GCM encrypted retained record before removing that old stage from
  active storage. A failure rolls back the entire transaction.
- The existing public consumer performs parsing, embedding, derived writes,
  atomic publication and authorized SQL retrieval. A ticket ID is a selector,
  never a source/content capability. Archived old tasks are never reactivated.
- Reconciliation settles expired or completed claims even after admission is
  disabled. Transient pre-admission failures have a bounded retry budget;
  security/authorization refusals do not automatically retry.

## Deployment interlocks

New behavior is disabled by default. A bounded deployment requires the existing
`FEISHU_PERSONAL_EXACT_RECOVERY` and exact ID allowlist, plus:

- `FEISHU_PERSONAL_EXACT_RETRY=1`.
- `FEISHU_PERSONAL_EXACT_RETRY_GENERATIONS`: a JSON map of approved original
  Knowledge IDs to their currently reviewed sealed generation (at most eight).
- `FEISHU_PERSONAL_EXACT_RETRY_UNTIL`: an explicit future RFC3339 expiry, at most
  one hour away. There is no automatic renewal or global allow-all default.

Each file still requires its own complete live proof. A new terminal execution
requires a new review; changing a file's displayed status cannot authorize it.

## Retention and rollback

This uses existing `task_pending_ops` scopes and needs no schema migration.
Quarantine is retained without an automatic cleanup/export endpoint. Preserve
the existing encryption key with protected backups. Any later erasure or reader
must have its own authorization and retention review.

Stop admission and fence/settle all live replacement claims before changing
backend versions. Never restore an old database over newly published data.
The published receipt format is unchanged; the private retry/audit/quarantine
records must be preserved. A frontend-only rollback does not alter execution.

## Known incomplete product capability

Published exact-receipt documents still cannot safely be handed to the legacy
incremental publisher: it changes source ownership/metadata while existing exact
readers bind the original source and published database fingerprints. The
source-specific UI must report this restriction; it must not invoke destructive
ordinary reparse. Closed Safety yields a temporary-unavailable response, and
an open gate alone does not authorize a versioned handoff.

Implement and test that receipt/publisher transition before claiming that
"sync source document again" supports changed published source revisions.
Existing published contents are never cleaned by this change.

## Evidence boundaries

The original DOCX isolated Lite run exercised a sealed generation 5 to fresh
generation 6, real configured model HTTP, 4 chunks/4 vectors, atomic publication,
normal detail/chunk handlers, authorized hybrid search, denied users, live grant
revocation, and duplicate delivery. OAuth authority was synthetic and isolated;
it is not production OA or production source-authorization acceptance.

Production remains a separate result: 22 published, two isolated as of this
candidate. The summary-only frontend release does not count as file recovery.
