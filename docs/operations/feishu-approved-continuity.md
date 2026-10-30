# Approved Feishu sources after incident recovery

This is a separately reviewed extension of PR #63, based on
`1568ffc3d9928527eb96077eb22c949f1a3d7a19`. It is not production approval,
a general queue resume, a new source permission system, or a held-source release.
No database migration is introduced. It does not change the 24 pending imports.

## Runtime and admission

Deploy the extension's exact reviewed commit/image with
`FEISHU_APPROVED_SYNC_LANE_ENABLED=1`, existing automatic Safety enabled and
the restricted Ed25519 public verification key. The private signing key remains
on the independent approver's host, outside the application/worker host.
When enabled, startup must seal `approved_sync` before consumers can start.
Missing Safety, strict mode, failed sealing or bad recovery-key configuration
aborts startup. Without the feature flag, workers do not consume this lane.
The original `sync` lane remains paused.

After the separately approved reset and successful single-source canary, review
the `awaiting_review` state and request a continuity plan. An additional offline
signature is mandatory to transition to `approved_sources`. A canary approval
cannot perform this operation. The policy allows 1–4 explicitly selected sources
for at most one hour. It binds the current runtime, worker incarnation, controller
fingerprint, membership revision, prior recovery plan and mode, original queue
field bytes, source configuration and authoritative permission fingerprints.

The HTTP operator must be a web-session system administrator with normal
tenant/KB authority for **every** source in the request. An API key, platform
administrator flag alone, or an approval signature does not grant KB access.
This initial HTTP entry point permits only sources accessible in the operator's
current tenant. It does not invent cross-tenant/private ownership rights.

Activation rechecks actual Asynq consumer identity, unchanged six-task historical
queue, live source facts and empty dedicated lane. Redis WATCH/CAS protects the
matched lifecycle. One Lua operation persists the signed approval and full plan
before changing mode and unpausing only `approved_sync`; audit failure leaves it
closed. No historical task is copied, removed, archived or replayed by activation.

Manual sync and cron use the same admission function, then the existing SyncLog
creation, Asynq deduplication/retry rules and SQL Claim. Consumers reconstruct the
policy context from authoritative source data, never task-supplied approval.
Each outbound request/budget use requires a matching DB-issued execution identity;
the authoritative loader verifies its current claimed SQL generation and running
state. It also rechecks configuration, source state, live membership/grant epochs
and connection identity. Existing remote ACL checks, SSRF validation, request
budgets, source versions and database publication fences remain in force.

Transient source `error` preserves existing retry semantics only while the same
source policy and all authority checks remain valid. A new approval still requires
`active`; pause, hold, revoked, deletion, authority/config changes and stale claims
do not become retry authority. Expiry, new trip, lost consumer, controller/storage
failure or missing audit stops/fences work and pauses both lanes. Renew only after
the private lane is empty and source executions/publications have drained; renewal
requires another signed plan. Never extend a lease or reuse an approval.

## Separate operating acts

Use Python 3.9+ with a maintained `cryptography` installation on the managed Unix
operations/approval hosts. All secret files must be owner-only regular files.
Use HTTPS or an authenticated loopback SSH tunnel. No authenticated redirects,
implicit signing, automatic retries, or direct Redis commands are provided.

1. Obtain the existing incident recovery plan and complete separately approved
   Reset and Canary using `scripts/ops/feishu_recovery.py`.
2. Save a new continuity plan with `scripts/ops/feishu_continuity.py plan`, supplying
   `--original-plan-file`, the exact runtime SHA, original incident identity,
   `--source` route anchor and repeated `--approved-source` IDs. Plan creation uses
   POST with an explicit source selection but does not mutate recovery state.
3. A separate approver runs `approve` offline with the saved `--plan-file`, reviewed
   evidence manifest, `--approval-key-file`, reference and bounded validity.
4. After explicit production continuation approval, the executor runs `activate`
   with that saved plan and `--approval-file`. Record the acknowledged state,
   durable control audit, source list/expiry and unchanged original task digest.
5. Verify one normal manual sync and one persisted cron tick for each approved
   source, including SyncLog, claim generation, publication, authorized retrieval
   and unauthorized denial. Add sources only through a fresh reviewed plan.

All commands additionally require the exact namespace, trip, incident, expected
historical pending count, runtime SHA and a new exclusive `--output` file. API
commands require the web session file and base URL. Approval does not contact the
application. A lost acknowledgement requires status/audit reconciliation before
any further action.

## Historical GitLab task and held source

The six historical shared-lane tasks remain intact. This extension intentionally
does not provide a global resume or claim that the legitimate GitLab wait has
executed. Its recovery requires a separately reviewed per-task delivery act:
pin task ID/full payload hash, GitLab source/config, original SyncLog/tenant,
unclaimed generation, current permissions and the five canceled Feishu identities;
invoke the existing processing service once under normal Claim, preserve the
original task for audit and prove canceled/stale deliveries cannot publish.
Until that act is implemented, tested and explicitly approved, keep the shared
queue paused. Never clear the GitLab task or create a replacement logical run.

Source `6a40e468-f985-40b0-a5e1-43274c2d785c` is not an eligible source while held.
Current evidence shows no SyncLog or enrolled legacy resource, but that alone
does not establish authorization or justify a state change. Its directory/app
permissions and complete historical bindings must pass a normal controlled
reconciliation lifecycle. This policy cannot release hold or accept it into the
approved map. Do not SQL-update state, replace the source, or remove history.

## Stop and rollback

Use the existing supported stop-only controller operation for the exact incident
namespace. Confirm a persistent trip, no permit, both lanes paused, drained local
requests and SQL execution fencing. Capture all approved-lane tasks and generation
outcomes before rollback. Never delete approval/audit/state keys or reuse an old
canary. A failed acknowledgement is an unconfirmed stop until reconciled.

Rolling back to the old production image after Reset requires the persistent
trip and both lanes paused first: the old image cannot enforce the new recovery
policy. Do not start an older consumer against an open lane. Database rollback is
not normally needed (no migration); restore snapshots only under a separate data
recovery approval and never overwrite newer valid publications blindly. Preserve
the five canceled Feishu entries, GitLab wait, all source holds and audit history.

## Validation scope

The new tests exercise Ed25519/CAS approval, live authority changes, missing/old
execution identities, shared budget admission, consumer loss, expiry, immutable
historical queue, real Asynq producer routing, cron admission, SQL generation
checks and HTTP tenant/private isolation. They use synthetic authorizations and
owned disposable Redis/PostgreSQL. Prior real-cloud PR #63 acceptance is attributed
to its recorded commit, not relabeled as a real-cloud run of this extension.
Independent review, exact extension commit/image validation and its deployment
approval remain separate gates.
