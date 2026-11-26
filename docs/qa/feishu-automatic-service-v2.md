# Feishu ordinary service and automatic recovery v2

Baseline: main `6ee9f128c4fc5ea62974995a0c725213f8f2c56b` (production is
still `adf38741588f65eee034062dea7f5f81a12d27a4`). This is an implementation
and qualification record, **not a claim of production restoration**.

## Source constraints versus internal process

| Existing code | Present behavior | v2 ordinary-service requirement |
| --- | --- | --- |
| `automatic.go:automaticStep`, `controller.go:freeze` | A failed admitted consumer snapshot latches a namespace-wide trip. `automaticPermitLua` rejects the trip even after the consumer is visible. | Withdraw execution immediately; classify and observe transient faults across a bounded stable window. Real worker loss/conflicting identity remains closed. |
| `normal_service.go:verifyNormalAuthorization` | The platform's normal-service grant itself needs an Ed25519 signature. | Ordinary service derives authority from current user/application and database policy. No offline signature or platform approval. Retain old grants as historical evidence. |
| `normal_queue.go:QueueNormalSource` | A latched platform trip rejects a legitimate request before SyncLog creation. | Validate the caller/source/KB before accepting a durable task; a recoverable consumer outage delays execution rather than erasing the user's request. Unknown control/SQL state still denies admission. |
| `recovery.go:checkRecoveryPolicy`, `ConfigureFeishuRecovery` | Key-file configuration and expiring incident state can prevent the entire dedicated lane from starting. | Ordinary execution must not depend on an incident signature or an approval-key file. Emergency/operator stops remain independent. |
| `observation.go`, `approved_sources.go`, `steady_sources.go` | Reset, Canary, continuation and source expansion require separate signed acts. | System-owned Canary and incremental admission, bounded by live source qualifications, current runtime and existing budgets; no human permit cycle. |
| `normal_provider_recovery.go` | Narrow atomic recovery already exists for attributable provider failures; it excludes consumer absence. | Reuse compare-and-set and preserved incident evidence for a versioned recovery transition. Never issue an unconditioned trip delete or permit write. |
| `normal_service_database.go:NormalSourceFromDatabase` | Live tenant/source/config/owner/claim/trigger checks; active/error allowed. | Preserve these checks and enforce actual KB write authority. A source's error label alone is not a permanent block; paused/deleted/revoked/conflicting identity stays blocked. |
| `datasource_service.go`, connector binding, SQL claim/publication | Real OAuth/application access, external identity, generation, claim, receipt, resource/chunk/embedding publication fences and idempotency. | Preserve. Do not infer remote ACL from local configuration, reuse another user's OAuth, replay archived tasks, reset cursors, or mutate error into active outside normal sync results. |

The signed normal-service grant does not override a persistent trip: these are
two independent checks. Membership inventory, permit and cancellation are
namespace-wide, so a consumer error can fence all Feishu runs. Provider/source
errors already have isolation support in normal service; losing ordinary
authorization can instead send them back through the incident path.

## Implementation safety contract

1. Introduce an explicit versioned **system service state**, not a forged or
   unsigned record pretending to be an Ed25519 approval. Keep the existing
   namespace and retain all legacy grants, trips and incident audits.
2. Admission, health and source execution are separate. Every actual source
   operation and SQL publication still validates live source authority and the
   runtime fence. Task payloads cannot assert authority.
3. Stable observations must pin membership, worker/incarnation and real consumer
   identity/queues. Read failures reset stability and withdraw execution. No
   deadline increase, unconditional retry or healthy-container substitute.
4. Historical-stop migration must bind original evidence, current runtime,
   quiescent SQL claims/publication leases and Redis inflight/queue state, then
   audit and compare-and-set atomically. Unknown, operator, identity-conflict or
   integrity stops are not classified as automatically recoverable.
5. A system Canary is a fresh normal increment, not an archived replay. It must
   validate actual source access, receipts and publication before gradual
   source admission. A source-local denial cannot grant that source or stop
   other qualified sources.
6. SQL/Redis split failure, missing/corrupt durable state, unknown provenance,
   expired leases or conflicting generations remain closed. No manual Redis
   edits are part of migration or recovery.
7. Keep original incident evidence in an untrimmed migration journal; bounded
   live diagnostic streams alone are not permanent incident retention.
8. Validate live Consumer identity/queues using the pinned Asynq descriptor,
   then compare that read's exact bytes in the admission/budget CAS. Protobuf
   ordering and ordinary active-count changes are not incarnation changes.
   This still rejects a read/use race; it never reuses an unverified heartbeat.

## Qualification status

The candidate now wires the system protocol into the dedicated automatic lane,
ordinary queue admission, transactional worker claims, live source validation
and publication guards. Legacy signed administrative recovery endpoints return
410 for that protocol; no signing key is read to start ordinary service.

Migration binds the original trip's exact audit entry, both audit tails,
current physical consumer, leases and membership to an immutable SQL receipt
and an untrimmed Redis journal. SQL preparation durably retires the old signed
executable grant, retaining its history. A previous binary therefore remains
Feishu-closed on rollback; do not restore an old grant or Redis safety snapshot.
Known split commits are reconciled against their exact immutable evidence;
unknown/missing state is never treated as a clean install.

Canary candidates have a five-minute execution scope and a bounded publication
evidence limit. An unsuitable source is deferred without changing its cursor,
document state or business permissions. The next qualified source can be
selected after the prior SQL claim settles normally. Publication requires the
real version receipt, original knowledge, chunks and matching vector rows;
independent Wiki finalizing is not a reason to falsify the core publication.

Actual qualification to date (2026-11-26, Asia/Shanghai):

| Evidence | Result | Boundary |
| --- | --- | --- |
| Linux run 45719954080, source `2200ae1` | PASS | Early primitives only; not the later runtime wiring. |
| Linux run 45722804545, source `76bcd4f` | FAIL: 730 passing cases, 3 skipped, 18 failed | Shared real-Redis fixtures contaminated static Asynq keys across tests. Assertions/failure logs retained; corrected fixtures own separate real Redis processes rather than clearing shared keys or downgrading to mocks. |
| Linux run 45732230651, source `29133116f` | FAIL: native SQL bootstrap stopped the control package | An immediate additional healthy read incorrectly invalidated an established stable window. The test now checks the exact injected SQL error rather than accepting any precondition failure; remaining tests after the panic were not executed. APP admission 22 cases, claim 3 cases and vet passed separately. |
| Linux run 45733572901, source `c8fb86d0a` | FAIL | A new test's typed capability constants needed explicit string conversion. No runtime qualification claimed. |
| Linux run 45735034346, source `c83f8f6fd` | FAIL: race 770 PASS / 3 SKIP / 2 FAIL; claim/scheduler 12 PASS / 1 FAIL | Revoked Personal MCP scope was safely denied but misclassified as infrastructure failure. Two fixture assertions also failed: a real database CHECK correctly rejected an invalid mode, and JSONB normalized whitespace. Fixes retain the constraint and compare cursor JSON semantically. APP admission 22 PASS; vet PASS. |
| Linux run 45736914623, source `948ebcddd` | PASS | Includes actual Redis/Asynq + PostgreSQL bootstrap, SQL acknowledgment failures/restarts, same-incarnation Consumer conflict and incident CAS races. The later live-byte proof correction still requires its own exact-head run. |
| Linux run 45740113710, source `bcbd544cf` | PASS: race/control/access 812, SQL/claim/scheduler 13, APP wiring 22; zero skips | Includes live protobuf validation/read-use CAS and all real Redis/Asynq gates. Final review subsequently added explicit rejection of legacy reconciliation purposes; that final head requires another exact-source run. |
| Native Windows control/safety/access regression | PASS | Real CGO and stack protection retained by linking the installed libssp. No configured private PostgreSQL or Redis executable; gated cases are not database/native-consumer acceptance. Later changes need rerun. |
| Final candidate Linux race, SQL, real Redis/Asynq and APP wiring | Pending | Use the exact committed head; earlier run results cannot qualify uncommitted runtime changes. |
| Full native APP acceptance and production restoration | NOT_RUN | No production safety-state changes, archived replay, forced finalization, or Wiki tuning. |

PR #266 is separately merged into `6ee9f128c4fc5ea62974995a0c725213f8f2c56b`;
no batch/map increase or production change is implied by that merge.

## Native build input availability

Native run 45735356985 failed while downloading the existing fixed uv 0.12.15
browser release URL (HTTP 403); native runtime tests and packaging did not run.
Its official GitHub Release API asset identifies the same pinned SHA-256.
The release Dockerfile now supports an optional read-only
`verified-release-assets` context. Its default empty stage keeps the previous
download behavior; a prefetched archive still passes the identical hard-coded
version/hash checks. No dependency version, native build or scanner is disabled.
This uses Docker's documented [named context stage override](https://docs.docker.com/build/ci/github-actions/named-contexts/).
Qualification prefetch confines the token to the official API request and uses
a fresh unauthenticated request after validating the release-asset redirect.
Neither the token nor an alternate unverified dependency enters the build.

## Historical task purpose boundary

The system protocol explicitly rejects legacy signed reconciliation purposes
before reading their old ticket. A valid ordinary-source check cannot replace
the old purpose's independent signature, expiry or publication guard. Retained
old tasks/tickets remain audit evidence; they are not reinterpreted or stripped
into new authority. Missing current content must use a fresh authorized ordinary
increment. Regression tests retain a real signed fixture and ticket, verify its
legacy behavior first, then verify all new-protocol entry points reject it
without changing the ticket, recovery record or queue.
