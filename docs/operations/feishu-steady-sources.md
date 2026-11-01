# Multi-space normal Feishu synchronization

This extends the existing recovery controller and `approved_sync` delivery
adapter. It does not change connectors, legacy source protection, SQL Claim or
atomic publication. There is no database migration or global Resume operation.

## Two separate lifetimes

Incident Reset/Canary and the original one-hour continuity API are unchanged.
Normal synchronization uses a new Ed25519 `steady_sources` authorization, an
explicit source list, a monotonically increasing policy revision, and a signed
authorization period of 1–30 days. The approval must be consumed within 15
minutes. A consumed approval cannot be replayed or extend its authorization.

The controller renews a **15-second health lease**, only after the existing
audit, worker/consumer inventory and permit checks succeed. The lease never
changes source IDs, permissions, resource selection, deletion policy or the
authorization end. Losing this health lease causes a persistent safety stop;
the controller cannot recreate it from an old approval. Policy renewal uses a
new signed replacement before authorization expiry, without an incident Reset.

## Independent source authorization

Every entry binds tenant, KB, source ID/type/state, config and authority hashes,
the actual approving user and its active membership. Each target is checked
independently against the database in planning, activation and execution.
Platform-admin status does not substitute for membership or private ownership.
An owner/admin may manage normal sources; a Contributor requires KB creation or
an authoritative source-owner relationship. Another user's private KB is denied.

Initial admission requires `active` and effective source authorization. An
already admitted source may retry a transient `error` only through the existing
`RetryEligible` predicate, unchanged authority/config and current SQL execution
generation. Hold/revoked, paused, deleted, revoked membership, stale claims and
changed source binding remain denied. Revocation of one entry does not trip the
other entries; global infrastructure/safety failures still close the gate.

## Operational sequence

1. Read current incident, recovery state, permit, queues and worker identity.
   Do not reuse an expired plan or pre-deployment incarnation.
2. Use normal administrator Web identity and the established independently signed
   Reset and Canary APIs for the current incident. Preserve all shared tasks.
3. POST `/:id/safety-recovery/steady/plan` with explicit `sources` and
   `authorization_days` (default seven). The anchor is still authorized normally;
   target-space authority is checked separately, with no client tenant grants.
4. Review each entry and sign using `scripts/ops/feishu_steady.py approve` on the
   offline signing host. POST the exact plan/approval to `steady/activate`.
5. Only `approved_sync` opens. Existing manual and cron producers use the same
   source admission; workers still acquire real SQL Claims. Verify actual file
   updates, parsing, index publication and user-authorized retrieval separately
   for Wiki and Drive. A zero-change success is not full content acceptance.

A signed replacement can add newly authorized normal sources or remove existing
entries. Replacement/addition waits for an empty dedicated lane. A strict
revocation-only subset may proceed with queued/active deliveries: it neither
removes them nor changes their identity; old delivery contexts immediately fail
their policy binding. Revocation never extends the existing authorization end.

The first production scope is exactly:

| Tenant | Type | Source |
|---|---|---|
| 10002 | Wiki | `14433cdb-fddc-4d6c-a979-ef11c05b61bf` |
| 10002 | Drive | `867e5798-1c2b-4547-921d-d43132e3f275` |
| 10005 | Drive | `5c7bc873-0ba3-43c7-90a0-32c23e6bd529` |
| 10018 | Wiki | `8a5e2f6f-b646-4f4e-8b8e-3eb021730182` |

This list is operational scope, not an embedded code grant. Three error sources,
ten hold sources and two previously paused sources are not included. Existing
source resource selections and deletion policies remain authoritative.

## Rollback and retained tasks

Normal mode writes recovery **version 2**. Old readers accept only version 1 and
therefore fail closed; do not change the version to make a rollback appear open.
Before rolling back, close/fence execution with the existing stop controller and
verify claims have terminated. Preserve the policy, approvals and audits. Do not
restore an old database/Redis snapshot over new business writes. A replacement
process cannot inherit a prior incarnation's open execution authority.

The shared `sync` queue remains paused. All six historical tasks are retained
byte-for-byte; five canceled Feishu tasks are not replayed, and the GitLab task
is not removed. Normal-mode status redacts cross-space certificate entries from
the anchor-only status route. No Token, credential, raw URL or content is added
to audit records.
