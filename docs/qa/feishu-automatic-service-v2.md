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

## Qualification status

Architecture/source audit in progress. New v2 code, integration, fault-path CI,
isolation acceptance and production rollout are **NOT_RUN** until recorded with
their actual source SHA and evidence. PR #266 is separately merged; no Wiki
batch/map increase or production change is implied by that merge.
