# Feishu incident-bound recovery (phase 1)

This entry point is disabled unless the running automatic controller has a
separate, regular, non-symlink, owner-only approval key file configured through
`FEISHU_SYNC_RECOVERY_APPROVAL_KEY_FILE`. Do not reuse SYSTEM_AES_KEY, an OAuth
secret, a Redis password or a key from another environment.

The deployment must be reviewed and approved separately. Installing this code
is not authorization to reset a trip or run a source. Phase 1 has **no queue
resume** operation. It does not recover the unrelated 24 pending imports.

## Runtime and authority

Routes, all restricted to a real authenticated SystemAdmin web principal with
normal access to the source's owning tenant and private KB:

- GET `/api/v1/datasource/:id/safety-recovery/plan`
- GET `/api/v1/datasource/:id/safety-recovery/status`
- POST `/api/v1/datasource/:id/safety-recovery/execute`

API keys cannot invoke these operations. Being SystemAdmin does not grant
another person's private KB. The status route cannot inspect another source's
recovery state. Responses contain identity, fingerprints and control state,
not source configuration, credentials or document content.

The plan binds namespace, trip and first/latest matching trip audit ID,
application SHA, worker ID/incarnation, controller fingerprint, membership
revision, full pending task byte digest, pause value, previous recovery state,
source config hash and live authority facts. The queue must be paused, at most
64 pending tasks, 1..64 explicitly selected source resources, and have no active/retry/scheduled/archived entries. This
implementation deliberately requires a single registered, live worker. Redis
must support the standalone multi-key scripts; Redis Cluster is not supported.

Facts additionally verify a live tenant/KB, no claimed Feishu executions, no
current live publication leases, installed knowledge/chunk/embedding fences,
membership/user status, private owner, application configuration and OAuth grant
epoch. Local facts are not evidence of a real remote ACL: normal source binding,
cloud authorization and revision checks still apply in the normal service.

Both reset and canary need their own HMAC approval. Approvals reference reviewed
backup/restore, isolation, source authorization and runtime identity evidence.
The evidence digest proves integrity, **not that tests or backups happened**.
An independent reviewer must verify their contents. A plan is valid for at most
15 minutes. There is no extension, automatic approval or retry after lost ACK.

## Closed lifecycle

1. Read-only plan; old trip and old Guard remain closed.
2. Approved reset: locally cancel/drain, apply normal SQL execution fencing,
   compare live facts again, then one CAS checks incident/topology/queue bytes.
   Persist audit and a non-expiring recovery-required marker/recovery policy;
   replace the worker incarnation; remove only the matching trip last.
3. Install a **fresh** Guard object. The old object is never reopened.
   Controller health must issue a fresh short permit. Frozen mode still denies
   normal producers and execution.
4. Separately approved canary: atomically compare queue bytes/pause/permit and
   admit only the named source/config/plan. ManualSync creates a normal durable
   SyncLog. A private delivery adapter captures exactly that new task, binds its
   run ID and generation 1, then calls normal ProcessSync/Claim directly.
   No task is inserted into or removed from the paused shared queue.
5. The normal parser, source binder, CAS, lease and database publication fences
   remain responsible for content publication. No source state is edited by the
   recovery manager. Normal publication may perform its existing legitimate
   source state/cursor transition; hold/revoked sources are never eligible.
6. A watcher checks source authority/config, trip and queue identity every
   200 ms. Authorization change, a new trip, audit error, cancellation, panic,
   deadline or unconfirmed outcome stops execution and invokes normal emergency
   fencing. The watcher supplements rather than replaces per-request and
   publication checks; it is not a distributed atomic ACL transaction.
7. Success moves to `awaiting_review`, which denies further work. The shared
   queue and all historical task identities remain intact and paused.

Recovery mode is persistent. Expiry denies work and the configured controller
trips/fences. A missing mode or binding marker denies work; the replacement
Guard also remembers that recovery is required. Never delete these records to
return to ordinary operation. A process restart does not authorize a pilot.

## Compatibility and release gates

No database migration, trigger change, schema 3043/3044 dependency or historical
Key/import mutation is introduced. Base is production
`962c7c888c99e8f13d64e5d2568fb4ff3ca0b171`; PR #61/#62 are not included.
Normal producers use the existing queue adapter. Recovery uses the same service
admission, execution claim and publication code with a different delivery adapter.

Required before production: current remote ACL/revision and bounded resource
scope, actual-model parse/index/publication and authorized/unauthorized retrieval,
revocation and late-generation rejection, Redis outage/audit failure, fresh
backup restoration, candidate image identity and rollback rehearsal. Unit tests,
synthetic embeddings or DNS/TLS success do not replace these checks.

## Stop and rollback

Preapprove an emergency stop for the pilot. Use the supported stop-only
`feishu-sync-control -action stop -namespace <approved namespace> -execute`
under the reviewed deployment's restricted environment. Confirm persistent
trip, absent permit, paused queue, zero in-flight/claimed execution and sealed
publication leases; an ACK alone is not proof of drain. Preserve all audit,
recovery policies and six original tasks. Do not issue raw Redis DEL/resume.

**Before rolling back to the old production image, establish and verify the
persistent stop.** The old image does not understand recovery-mode restrictions;
rolling it back after a reset without a confirmed trip is unsafe. Quiesce and
fence before replacing a process. Do not restore an entire database over other
users' writes to undo a successful publication. Inspect the normal source
version/cursor and approve a separate version-aware repair if needed.

## Subsequent shared queue phase (not implemented here)

After the pilot is reviewed, separately approve queue restoration. Reconcile
all six task IDs and payload digests: five Feishu runs must remain canceled,
unclaimed and fenced; the GitLab run must preserve its original identity and
valid execution conditions. Test late delivery against normal DB Claim before
resuming any queue. Establish an approved source allowlist and admission policy
for every producer/worker, not merely an environment variable on one worker.
Do not resume the shared queue while relying on this one-run recovery policy
or remove that policy to enable all sources. A later reviewed transition must
atomically install the restricted ongoing policy and audit it. This CLI does
not offer that transition or imply that global service has already recovered.
