# Feishu P1 audit and isolated implementation

Base: accepted P0 `70c7d56765e1b1ca2a6e3d33c8d7047a15422c71`, dependency only; no change to PR #35. No production access, sync, image release, merge or deployment is authorized here.

## Audit before edits

- `feishu_legacy_staging.go` rejects parent-child configuration. Ordinary ingestion (`knowledge_process.go`) creates parent_text context rows without vector/BM25 entries, and child text rows referencing those parents. Parser-defined segments/chunks have established splitting semantics which must be reused.
- `feishu_legacy_atomic.go` assumes chunk count equals vector count in Prepare and Publish. This prevents complete parent/child manifests. Extend one manifest to contain all chunks, while its vector candidates cover only retrievable text chunks. Validate all parent and adjacency references within that manifest, never resolve a parent from a previous version. Preserve existing single-transaction public replacement, triggers, execution claim, resource generation, lease, model/storage stamps and receipt checks. No migration is needed.
- Begin compares an incoming revision to resource.SourceRevision but checks equal-timestamp changed content against active.Revision. A duplicate-content observation advances resource.SourceRevision without changing active.Revision; subsequent changed content at that same high-water timestamp can pass the older comparison. Reproduce and reject the ambiguous same-high-water revision. Do not relax source rollback, ownership or lease guards.
- Existing generic fence errors conflate revision ambiguity, execution/generation ownership and expired leases. Add typed, credential/body-free reason codes and safe IDs/generations for diagnosis, preserving errors.Is compatibility. A local reproduction cannot establish the exact production cause of the 流程地图 error.

## Validation plan

Real disposable PostgreSQL/ParadeDB tests cover parent and child counts, child-only vector/BM25 rows, stage invisibility, old version protection after embedding/publication faults, complete graph replacement, stale owner/lease, concurrent attempts, retries, repeatable-read visibility and timestamp high-water ambiguity. Reuse the production parser/embedding HTTP harness and failure injection. Full backend and race run in isolated CI. No production credentials or source content are used.

## Implementation and compatibility

| Artifact | Version contract |
|---|---|
| Parent text | Fresh IDs per candidate; persisted in the same manifest/transaction; context only, not embedded or in BM25 |
| Child text | Fresh IDs and same candidate parent references; existing splitting/configuration and parser-preserved standalone chunks retained |
| Vector and BM25 | One candidate/index row per retrievable text child; parent rows excluded; input order follows child order |
| Manifest | Contains the complete graph, headers and model/storage/configuration stamps; digest checked again at publication |
| Receipt | Counts all public chunks and only indexed child vectors/BM25 separately; existing receipt shape unchanged |
| Publication | Existing datasource/sync execution/resource/knowledge locks and database triggers retained; one PostgreSQL commit replaces the current graph/indexes |
| Older versions | Existing soft-deleted Chunk history is retained; normal reads exclude deleted rows. Old IDs are never attached to new children |

No database migration is required. No user chunking or permission configuration is changed. Parser-defined segments and preserve-parser-chunks policies continue using existing materialization rules; media/enrichment paths remain closed if their derivative contract is unsupported. Validation rejects incomplete graphs, unknown chunk kinds, foreign parents, cross-document/tenant references and references outside the candidate.

The revision fence binds the resource high-water timestamp to the current attempt hash when that attempt owns the high-water, otherwise to the active published hash after a duplicate-content timestamp advance. This protects both initial unpublished candidates and previously published documents. Exact-byte retry at a failed candidate's timestamp remains admissible after its owner lease is released; a changed hash at that timestamp remains ambiguous and is rejected. A later revision remains necessary to confirm changed source content. No document content or hash is logged.

## Version fence trigger inventory

`feishu_legacy_atomic.go` now returns typed `LegacyFenceError` values while preserving `errors.Is(ErrLegacyVersionFence)`. `StageFeishuLegacyItem` emits `feishu_version_fence` with a stable reason, tenant/source/log IDs and execution generation only.

| Path | Trigger / reason families |
|---|---|
| Store/execution admission | Unsupported repository; missing model/storage binding; missing/wrong execution context; source paused or configuration changed; inactive tenant/KB; missing running claim or different execution generation |
| Begin | Missing/invalid revision; duplicate enrolled source identity; revoked resource; revision regression; same-high-water content conflict; unconfirmed high-water hash |
| Ownership | Expired resource lease; changed current attempt, resource generation, owner generation/nonce, terminal attempt or revocation |
| Prepare | Invalid manifest shape, identity/source binding or graph; wrong vector count/dimension; nonfinite/out-of-range values or zero norm; attempt not staging |
| Publish | Attempt not ready; manifest digest mismatch; changed KB/model/storage/file binding; unavailable HNSW dimension; candidate count mismatch; wrong public chunk/vector counts |
| Receipt/cursor | Inactive/superseded receipt, wrong identity, changed public counts, invalid receipt or mismatched manifest |
| Quarantine/source edit | Missing/ambiguous restriction identity; forbidden tenant/KB reassignment (`feishu_legacy_quarantine.go`, `datasource_repo.go`) |

`ErrLegacyVersionBusy` separately means a still-live candidate lease. Database connection/lock/trigger errors retain their original cause; storage quota rejection remains explicit. These are not converted into permission withdrawal or treated as confirmation that old knowledge was deleted.

## Isolated evidence and rollback requirements

The dedicated P1 workflow first runs the new regression against the exact accepted P0 atomic implementation. It requires the expected missing-fence assertion, restores the fixed source and verifies no source diff before running repaired tests. The baseline reproduction on GitHub run `39946586024` showed the expected `in chain but got nil` failure; that run's matcher text was corrected in the subsequent candidate. The first workflow attempt had insufficient git history and was corrected. Neither result is presented as a passing fixed-source validation.

The final workflow must pass: real PostgreSQL/ParadeDB version/parent tests; Wiki and Drive real-parser HTTP embedding tests with parent-child both off and on; middle-batch HTTP 400 retaining byte-equivalent old knowledge/chunk/vector JSON; eight concurrent competing Begins; publication fault after replacement starts; ready-attempt retry, abandoned-attempt exact-byte retry, lease expiry, execution generation change; real keyword/vector retrieval; repeatable-read old graph; race and full internal tests. Its artifact records the exact tested SHA and baseline reproducer log. The final PR body records dynamic CI URLs and results, without a follow-up documentation-only commit that changes the tested SHA.

On Windows, repository graph tests passed. Native service linking is unavailable because installed MinGW lacks `rstrtmgr`; the dedicated Linux CI is authoritative for service, PostgreSQL and race results. General App CI packaging steps are excluded for this P1 branch; Go test compilation is allowed, production executable/image building is not. Earlier automatic App runs were canceled before their Build server step; Docker jobs executed no steps.

Rollback to the approved P0 binary must keep synchronization frozen and drain/fence active executions first. The new code adds no schema or incompatible receipt fields; ordinary parent-child reads already exist in the baseline. A P0 worker cannot safely continue preparing parent-child updates and must not be restarted as a recovery worker. Preserve published knowledge and all prior candidates/history/files; never restore an old entire database over later successful writes. Isolation tests validate rollback of failed database publication, not a production image rollback or production recovery acceptance.

## Release boundary

This is an independently reviewed dependent P1 change. Production remains NO-GO until P0's external quota, topology, automatic stop and source qualification blockers are resolved. The 2026-10-27 19:25 Asia/Shanghai legacy-source stop deadline is not extended by this work; if recovery acceptance is incomplete, preserve old sources/backups and seek an approved stop-window extension, without cleanup or automatic extension.
