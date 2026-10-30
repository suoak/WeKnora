# 2026-10-30 incident execution dossier — NOT APPROVED TO EXECUTE

Incident identity:

- namespace: `feishu-prod-080f7bb6a01f`
- original trip: `feishu_headers_io_failure`
- original event: `1793268000119-0` (2026-10-29 18:00:00.119 CST)
- incident APP: `962c7c888c99e8f13d64e5d2568fb4ff3ca0b171`
- candidate source: `5c7bc873-0ba3-43c7-90a0-32c23e6bd529`
- candidate KB: `6dcae886-fc03-446d-9523-ee6f4e5009c9`, tenant `10005`

Read-only refresh at 2026-10-30 11:25:38 CST: candidate active/unmanaged,
not deleted, zero claimed executions, zero legacy resources, last success
2026-10-29 12:00:04 CST. This is not current remote authorization proof or
an execution approval. Exact candidate resource/config/version must be reviewed
under normal authorization before any production action.

## Evidence and unresolved first cause

Production log-window refresh found zero retained APP lines around the incident;
frontend retained 835 lines without classified I/O/TLS/DNS/5xx evidence. Docker
and nginx service journal returned zero lines in that window. External gateway
logging outside this host was not available. The original request type, source
and network phase therefore remain unproven. Do not state that the root I/O fault
has been repaired.

Current direct diagnostic from APP: Feishu DNS had 16 global answers and valid
TLS certificate negotiation. The model-gateway answer is an internal/restricted
address; the diagnostic deliberately did not connect to it. That alone is not
a product SSRF error: the configured internal endpoint must be tested through
the existing approved application SSRF policy, credential and model transport.
No whitelist was widened. DNS/TLS success does not prove ACL or embedding health.

The test-site real ACL/revision/view/copy/export preflight passed after explicit
approval of a one-hour process-only exact callback policy. Initial execution
without renewal correctly failed. Full recovery-controller + actual-content
cloud/model end-to-end acceptance remains a separate release gate; see evidence
manifest and actual test results, never infer it from unit pass counts.

Read-only safety refresh at 2026-10-30 12:16:47 CST confirmed the same trip, absent
permit, paused sync, pending 6, active/retry/scheduled/archived 0 and no wire slots.
No recovery operation was run on production.

## Required production approval packages

1. **Build/deployment approval**: exact runtime commit, reviewed diff from
   incident APP, candidate image digest and provenance, SBOM/CI, effective config
   hash, restricted independent approval-key mount, controller single-worker
   topology, fresh worker incarnation and process identity. No PR #61/#62 or
   original-import recovery changes. No DB migration is required by this change.
2. **Reset approval**: freshly generated plan against the actual installed
   runtime, current incident still matches, candidate scope and config unchanged,
   source authority verified, all old executions drained/fenced. Approver signs
   only after accepting all four evidence categories. Reset keeps sync paused.
3. **Single-source canary approval**: a second explicit short approval for the
   same plan/source, exact selected resource list and bounded expected content,
   quota/budget and expiry. Approve emergency stop authority for this pilot.
   This approval does not cover other sources, grants, old tasks or queue resume.
4. **Global queue approval**, later: separate task reconciliation and ongoing
   approved-source policy. Not implemented or implicitly authorized by phase 1.

The tool validates signatures and snapshot identity, not human review truth.
Do not generate production approvals from synthetic evidence or carry over
historical approval files. A live plan expires within 15 minutes; repeat review
when identities/queue/config change, not by editing the saved JSON.

## Backup and restore prerequisites (not executed in production)

Immediately before the approved window, use the normal backup process to
capture a consistent PostgreSQL backup (migration version + triggers + source
resources/attempts/versions/claims), original file/resource storage snapshot,
Redis persistence/queue task hashes and all safety audit/control streams,
current compose/config fingerprint and exact APP/frontend images. Encrypt and
restrict access; do not print credentials or raw task payloads in the dossier.

Restore into an isolated network with no APP workers, outbound mail/schedulers
or production credentials in an executable process. Check schema/trigger state,
source/attempt/current-generation/cursor identities, file hashes, all six task
IDs and binary payload hashes, queue pause, Redis audit IDs and namespace.
Record backup timestamps, artifact SHA256, restore command/exit status, row/hash
checks and measured recovery time. Existing older backups do not establish
this incident's recoverability. No production backup/restore pass is claimed.

## Stop/rollback and monitoring

Before rollback to old image, establish a persistent trip and verify no permit,
paused shared queue, zero live wire slots, no claimed Feishu execution, and
fenced publication attempts. The old image does not understand recovery policy;
never run it with only a frozen-mode key and no trip. Preserve all audits and
historical tasks. Use the supported stop-only controller, not raw Redis changes.
If stop acknowledgment is uncertain, retain the stopped state and reconcile
rather than retry reset/canary. Do not bulk-restore PG over unrelated writes.

Monitor the approved source/run/generation, task receipt, controller/consumer
lease and heartbeat, permit TTL, inflight slots, source grant/revision, audit
append health, queue digest and phase. Any change/new trip/unknown outcome
stops the pilot. Success requires normal SyncLog success, source version/cursor
consistency, no duplicate K/Chunk/Embedding, actual authorized retrieval and
negative authorization, plus unchanged other sources and six original tasks.
A metadata-only or zero-change sync is not proof of parsing new content.

## Shared queue reconciliation

Preserve the original five canceled Feishu executions and their generation
fences; late delivery must fail to Claim and cannot publish. Preserve GitLab
source `756abd45…`, original log `ae327091-c4bc-4b25-9f1f-e45a4ca62720`, and
its legitimate waiting identity/conditions. Resolve exact IDs from the retained
private snapshot; do not guess from abbreviated IDs. After pilot review, test
these deliveries in an isolated restore, install an ongoing allowlist policy,
and seek separate approval before queue resume. Phase 1 exposes no resume call.
