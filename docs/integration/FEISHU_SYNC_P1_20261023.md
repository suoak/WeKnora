# Feishu P1 audit and isolated implementation

Base: accepted P0 `70c7d56765e1b1ca2a6e3d33c8d7047a15422c71`, dependency only; no change to PR #35. No production access, sync, image release, merge or deployment is authorized here.

## Audit before edits

- `feishu_legacy_staging.go` rejects parent-child configuration. Ordinary ingestion (`knowledge_process.go`) creates parent_text context rows without vector/BM25 entries, and child text rows referencing those parents. Parser-defined segments/chunks have established splitting semantics which must be reused.
- `feishu_legacy_atomic.go` assumes chunk count equals vector count in Prepare and Publish. This prevents complete parent/child manifests. Extend one manifest to contain all chunks, while its vector candidates cover only retrievable text chunks. Validate all parent and adjacency references within that manifest, never resolve a parent from a previous version. Preserve existing single-transaction public replacement, triggers, execution claim, resource generation, lease, model/storage stamps and receipt checks. No migration is needed.
- Begin compares an incoming revision to resource.SourceRevision but checks equal-timestamp changed content against active.Revision. A duplicate-content observation advances resource.SourceRevision without changing active.Revision; subsequent changed content at that same high-water timestamp can pass the older comparison. Reproduce and reject the ambiguous same-high-water revision. Do not relax source rollback, ownership or lease guards.
- Existing generic fence errors conflate revision ambiguity, execution/generation ownership and expired leases. Add typed, credential/body-free reason codes and safe IDs/generations for diagnosis, preserving errors.Is compatibility. A local reproduction cannot establish the exact production cause of the 流程地图 error.

## Validation plan

Real disposable PostgreSQL/ParadeDB tests cover parent and child counts, child-only vector/BM25 rows, stage invisibility, old version protection after embedding/publication faults, complete graph replacement, stale owner/lease, concurrent attempts, retries, repeatable-read visibility and timestamp high-water ambiguity. Reuse the production parser/embedding HTTP harness and failure injection. Full backend and race run in isolated CI. No production credentials or source content are used.

## Release boundary

This is an independently reviewed dependent P1 change. Production remains NO-GO until P0's external quota, topology, automatic stop and source qualification blockers are resolved. The 2026-10-27 19:25 Asia/Shanghai legacy-source stop deadline is not extended by this work; if recovery acceptance is incomplete, preserve old sources/backups and seek an approved stop-window extension, without cleanup or automatic extension.
