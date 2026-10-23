# Integrated Feishu sync validation — 2026-10-23

Integration order: PR #35 → #37 → #36, based on main `b64ab3b6b6dc4dde12adba5dd929be26fb9a16c8`.

## Defects found by actual deployment

- Selecting one Drive file previously traversed unrelated folders. Selected direct children now stop discovery at the matching page; existing nested selection compatibility remains.
- Automatic shutdown registered safety cleanup before workers, so reverse cleanup stopped consumers first. Safety now drains before workers; normal departure fences execution and removes only its own incarnation. Unexpected loss remains latched.
- Any source failure triggered global stop in two service layers. Source permission/parser/version failures retain normal task retry; gateway, Redis, control failure and unclassified panic remain global stops. Invalid Embedding results are stopped at the model boundary.
- Repeated trips overwrote the initial cause and filled the bounded audit stream. First cause is retained; duplicate trips still close permission. Diagnostics use fixed categories and omit credentials/content.
- A first unpublished resource was enrolled as access `hold`, hiding already published siblings. Only first resources without historical public knowledge enroll as active; candidate content remains private until atomic publication. Existing hold/revoked/source quarantine and all read guards remain intact.
- Cron-only edits re-encrypted unchanged credentials with a new AES-GCM nonce, which the configuration fence incorrectly treated as a source identity change. Semantically unchanged configuration now retains its existing stored bytes; actual resource/credential changes still quarantine.
- The service adapter still required one vector per total chunk after parent/child integration. Receipts now explicitly distinguish parent and indexed chunks. Exact graph counts are required; old receipts are verified read-only against immutable manifests and live rows before returning compatible counts.

## Actual isolated environment

Dedicated compose project: `knowhub-feishu-integrated-20261023`, with its own PostgreSQL/ParadeDB, Redis, local file/staging volumes and network. APP and frontend bind only to packaging-host loopback ports 28180/28181. No production database, Redis, file volume or queue is attached.

Credentials are outside the repository, mode 0600. Only two selected existing Docx files are read. No existing Feishu document or permission is modified. Synthetic interface/fault tests cover other connector formats; actual authenticated acceptance in this session uses the selected Docx files.

The gateway rejected 20 synthetic inputs with HTTP 400 / InvalidParameter and explicitly reported a maximum batch of 10. The existing per-model `embedding_max_batch_size=10` setting is used. This is stricter than the shared Feishu maximum of 20. Official model documentation: https://help.aliyun.com/zh/model-studio/text-embedding-synchronous-api . Production must apply this model setting before restoring sync; a nominal RPM/TPM quota does not override the per-request model limit.

Shared test budget: in-flight 1, RPS 1, RPM 30, conservative UTF-8 input bound TPM 30,000; Redis is authoritative across workers. Online queries remain outside the background guard.

Observed results before final restart acceptance:

- Flat first/incremental recovery: two public documents, 162 chunks and vectors; successful terminal status, no failed items.
- Online vector retrieval returned both selected document identities.
- Real MCP initialize and search_knowledge returned successfully through an independently scoped test endpoint.
- Parent/child graph: 19 parents, 171 indexed text chunks/vectors; no orphan child, parent vector or missing child vector. Actual retrieval returned both document identities. A fresh first sync on `c7879adaf03a280431c5644a36b102586d514276` succeeded with 2 documents created and 0 failed.
- Isolated pg_dump restore succeeded into a separate template0 database: all five checked table counts matched (documents, chunks, vectors, resources, attempts). The restored database has no APP/Worker attached.

- Same-namespace real APP restart preserved identity summaries for all 6 documents, 542 chunks and 504 vectors; permission recovered without clearing a latch. The test error source executed successfully twice after restart; the explicit paused source stayed paused with zero executions.
- Deployed provider-independent emergency CLI latched stop, removed outbound permission and paused sync; manual admission was rejected, in-flight was zero and online retrieval from an unaffected knowledge base still returned 5 matches. A cron-edit quarantine defect was found separately and is being validated on the follow-up candidate.

## Release sequence

1. Finish exact-candidate lint, combined Redis/PostgreSQL/ParadeDB, race and complete backend CI. Confirm real fresh parent publication, deduplication, restart scheduling and explicit pause preservation.
2. Review integration delta, merge dependencies and integration fixes, then validate exact final main SHA. Build final images and run Startup Smoke from that SHA; test images are not production artifacts.
3. Preserve production old image/config and a verified recoverable backup. Enumerate actual Redis consumers and stop/drain every old sync-capable APP/Worker before restoring new sync. Set model batch 10 before restored scheduling.
4. Start the new runtime with sync admission closed, verify source SHA/config/budget/consumer membership, and verify provider-independent emergency stop. Restore only eligible active/error schedules; explicit paused/revoked/hold remain unchanged.
5. Use the conservative shared budget, restore normal eligible business scope, verify APP/Portal/OA/MCP retrieval, and retain source-specific failures for repair. Do not treat a source permission error as authorization to expand Feishu permissions.
6. On authorization/data/version integrity failure or uncontrolled outbound, close sync output, pause sync, cancel/fence/drain affected execution and preserve the old published version. Keep the stop latch until the cause is addressed. For application rollback retain snapshots, review migration compatibility and restore the old runtime with sync closed; do not automatically restart a worker that bypasses controls.

No production merge/deployment has occurred during the isolated observations above. Update this evidence after final acceptance and any actual deployment.
