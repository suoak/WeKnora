# Exact personal v3 file recovery

Status: development acceptance; not approved for production deployment or writes.
Production compatibility base: `539158a1f1c9024de8942d94d8f0ec8c9156e0e3`.
This change is independent of legacy Feishu Reset/Canary and does not reopen that gate.

## Execution boundary

The initial path accepts only user-OAuth v3 first-create DOCX protocols with a
single canonical local source-file binding, verified manifest/file hashes,
current connection identity, grant epoch, live remote ACL/revision and an
exclusive current generation/nonce/lease. No client field enables recovery.

`FEISHU_PERSONAL_MVP_FIRST_CREATE=1` retains the existing first-create protocol.
`FEISHU_PERSONAL_EXACT_RECOVERY=1` enables exact receipt readers and consumers.
`FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS` is an execution interlock: initially
only `62b4dfac-8e5c-46c9-be87-86493aca4d40`. It grants no source or read authority.
Do not enable other IDs before their live authorization and protocol checks.

The normal durable dispatcher and public `ProcessDocument` consumer retain the
original outbox, Knowledge ID, resource and protocol. No manual enqueue or SQL
state rewrite is part of this procedure. A fresh Claim precedes source-file
read; proof is revalidated at parser/model/write boundaries. Output stays
private until the generation/CAS transaction publishes the exact receipt,
original Knowledge and original outbox together. The nonce is consumed.

Receipts use the existing task_pending_ops schema, in a dedicated non-runnable
scope `personal_exact_receipt_v1`. They must not be drained, replayed or purged as
ordinary queue work. No new migration is introduced; the existing 3036 atomic
write guards remain enabled. This change does not reserve migration 3043/3044.
Forward/backward schema compatibility does not imply old-reader compatibility.

## Serving boundary and current limits

BM25/vector predicates authorize each exact document before selecting bodies.
They intersect published output fingerprints, current protocol/generation,
connection/grant, source-file binding and live viewer membership. Service
checks additionally revalidate remote ACL/revision and file hashes. Revocation
discards in-flight results. No KB-wide capability is issued.

Single-KB search fans out per exact receipt and recombines authorized results;
more than 256 receipts returns an explicit narrowing error. Fresh text RAG and
rerank rebuild actual inputs from canonical chunks and server templates. This
initial implementation supports a single proved document per model closure.
Mixed documents, unknown history/cache, media, attachments, external DOCX
relationships and embedded packages remain fail-closed. Multi-KB/MCP expansion,
Wiki generation and arbitrary historical conversation replay are not enabled.
A receipt reports historical execution; it is never current read authority.

## Required acceptance evidence

- Original fixture bytes and original IDs: Claim 3 to 4, real AnyDoc output,
  normal public consumer, real configured model HTTP, nonzero chunks/vectors,
  derived write and atomic publication, normal authorized BM25/vector/Chat.
- Real test OAuth ACL/revision/export and normal local disconnect rejection,
  on a disposable test DB clone; no production credentials or remote revocation.
- Ordinary viewer succeeds; forged administrator and non-member fail.
- Real 3036 guards reject old held Knowledge, Chunk and Embedding writes.
- Changed grant/generation/binding/channel, stale owner, expired lease,
  duplicate delivery and unknown pre-existing output reject safely.
- Queue adapters: document exactly which Redis/Lite paths ran; shared consumer
  tests alone do not prove two complete backend integrations.
- Exact candidate commit, necessary CI and image identity before release.

## Single production pilot (requires separate explicit approval)

1. Record current deployed SHA/image; back up current DB, resources, outbox and
   receipts without stopping or resetting unrelated legacy safety state.
2. Recheck the pilot's authoritative import/attempt/resource, original file hash,
   unique binding, current tenant/KB access, grant epoch and remote ACL/revision.
   Baseline op192370 and generation3 are observations, not forced new values.
3. Deploy the approved recovery candidate, retaining existing Safety features.
   Enable the server feature and the one-ID interlock. Let the normal durable
   dispatcher/reconciler deliver the existing pending operation.
4. Observe fresh Claim, actual DOCX output, chunk/vector counts, controlled
   summary/profile/metadata and atomic current-version publication. Validate
   authorized BM25/vector and fresh single-document RAG plus denied-user probes.
5. Confirm old held source remains denied, its rows unchanged, no duplicate
   publication and no change to the shared sync queue or its six historical tasks.
6. Stop on a fence mismatch, authorization/revision change, unexpected source
   dependency, unknown output, cross-space access or deletion. Preserve partial
   staging for review. Never replay it as an ordinary file.

## Remaining files and stop/rollback

Use the private `RECOVERY_EXECUTION_PLAN.csv`: pilot, two simple documents,
then the remaining fifteen simple candidates. Six embedded-object candidates
stay in dependency review until a complete proof exists. Every file requires
fresh ACL/revision/hash/binding/generation checks; failure does not authorize
fallback or block independently valid documents. Record parse/index/publish
and actual authorized retrieval separately; zero-content outcomes are explicit.

To stop new claims remove the execution allowlist, preserve exact receipt
readers, fence/drain matching in-flight executions through normal service
ownership and preserve audit/output rows. Local revocation/expiry remains
fail-closed. Do not flip hold/revoked, overwrite business data with an old DB
snapshot or delete unknown staging output. Old production539 readers do not
understand exact receipts: rolling back a binary after exact publication is
not an authorized way to reopen access. Keep ingress closed and use a compatible
reader build or forward fix until that rollback is independently validated.
