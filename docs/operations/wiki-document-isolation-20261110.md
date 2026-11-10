# Document-level Wiki recovery

Product authority: the operator requested that the latest readable Feishu
content supersede historical copies, inaccessible documents be skipped, and
individual failures never stop unrelated Wiki work. Import into the destination
KB supplies publication consent; no second importer authorization is requested.

## Implementation and rollout status

This change is in development, not deployed. Production remains on
`6852ff304d0abbae34fba6596f6b85950e97b427`. The new worker behavior is explicitly
gated by `KNOWHUB_WIKI_DOCUMENT_SELECTION=true`; strict existing callers remain
unchanged until the partial-document protocol passes validation.
`KNOWHUB_WIKI_DOCUMENT_SELECTION_KBS` must also list each reviewed KB ID. An
empty allowlist leaves every worker on the existing protocol, even if the
feature switch is on. Expand only after the read-only preview and canary.
An optional `KNOWHUB_WIKI_DOCUMENT_SELECTION_ACTIVE_FILE` contains a JSON array
of currently active KB IDs. The environment allowlist remains the upper bound;
the file cannot activate an unreviewed KB. Missing, oversized or invalid files
leave the new behavior inactive. Operators replace this file atomically to
expand a completed canary without restarting the APP or interrupting sync again.

Implemented candidate behavior:

- Independently preflight documents using current source binding, publication,
  parsed/enabled state and trigger-backed content epochs.
- Capture a generation proof over the eligible set. Model and publication
  boundaries recheck that exact set; unrelated excluded documents cannot stop it.
- Isolate pages whose complete stored proof is no longer valid. Retain the body,
  proof and identity outside the live slug namespace; requeue independently
  verified references for clean regeneration. Never re-certify historical text.
- Persist per-document skip reasons in `wiki_document_skip`. Keep original
  skipped ingest rows in `wiki_skipped_ops`, including ID, payload and retry count.
  Claim settlement compares the exact worker-owned timestamp.
- Park finalize rows in `wiki_waiting_finalize` if no verified documents remain.
- On recovery restore original rows, rather than creating replacement identities.
  Housekeeping rechecks a bounded 32 diagnostics per sweep after a 30-minute
  interval; it does not fetch remote content or grant source permission.
- Report skip counts and reasons through Wiki stats and the browser.
- Opt-in `KNOWHUB_FEISHU_LATEST_CONTENT=true`: handle a timestamp/content
  collision only after capturing a database generation and re-listing/re-fetching
  the current remote document. Compare complete package identities and source
  bindings, CAS the captured generation, and re-fetch again before publication.
  Keep the provider revision unchanged; record previous/new content hashes and
  local generation in the hashed attempt manifest. Ordinary strict calls retain
  their original collision rejection.
- The worker receives a server-only, KB-scoped read guard over its selected
  documents. This also covers production query predicates; skipped content is
  never granted ordinary workspace access.
- Browser GETs in reviewed KBs filter to currently proved pages. The response
  writer rechecks selected sources and the exact pages actually read. Unknown
  historical pages remain hidden; a page scope never grants raw document reads.
- `wiki-regenerate -preview-document-selection -tenant <id> -kb <id>` uses a
  PostgreSQL `REPEATABLE READ READ ONLY` transaction. It reports exact eligible
  and skipped document IDs, affected page IDs and original pending counts;
  it does not take a Redis lease, move pages or modify queue records.

Validated candidate (2026-11-10): SQLite and native PostgreSQL tests passed,
including production read guards, stale publication fencing, duplicate live
operations, non-mutating preview and browser output checks. Existing Wiki
history/selective recovery regressions, native Feishu atomic publication and
XLSX identity regressions passed. Race-enabled tests, `go vet`, server and
operator builds passed. Follow-up validation also covers historical JSON-null
directory paths, nested COUNT/projection reads, and retaining existing authority
for shared KBs and API keys. Frontend production build and all 16 locale checks
passed. Validation artifacts: `WIKI_DOCUMENT_SELECTION_V10_VALIDATION*.json`,
`WIKI_DOCUMENT_SELECTION_V12_VALIDATION*.json` and the hash-linked
`WIKI_DOCUMENT_SELECTION_V13_VALIDATION*.json` and
`WIKI_DOCUMENT_SELECTION_V14_VALIDATION*.json` in
the operator incident directory. This is validation evidence, not deployment
or a claim that production backlog has drained.

Important remaining work before release:

- Finish the deployed 3045 test-environment HTTP acceptance, reviewed production
  previews, backup verification and one-KB canary before broader rollout.
- Integrate personal-import current publication proofs. Until that is complete,
  personal documents are explicitly skipped, never admitted as generic local data.
- Finish source refresh/backfill, individual Excel failures and production audit.

## Acceptance requirements

1. Mixed valid, disabled, failed, personal-pending and missing-proof documents:
   valid siblings publish; every excluded input has a durable reason.
2. Mutation or permission withdrawal of an admitted document fences its old job.
3. Excluded-document mutations do not invalidate an unrelated selected generation.
4. Unknown old page text never reaches a new model request; original bytes remain
   available for operator audit.
5. Wrong tenant, mismatched payload identity and stale claim ownership cannot
   restore or settle an operation belonging to another execution.
6. An all-ineligible KB performs no model work and does not busy-loop.
7. Recovery restores original task IDs, payloads, attempts and retry counts.
8. Finalize and page serving enforce the same source rules as ingest.

## Production sequence

Prepare a bounded read-only preview and backup for affected KBs, their pages,
proofs, operations, source bindings and checkpoints. Record current image and
rollback image. Let current Wiki batches finish naturally. Do not interrupt any
new normal sync run without an applicable user-authorized exception.

Canary one affected KB, verify model requests/publication/skip diagnostics,
then expand to the remaining KBs. Preserve all original queue/archive history;
do not clear queues or set parser/publication success by operator SQL.

Rollback disables the new generation behavior and restores the prior image.
Already isolated pages and retained skip rows need an explicit reviewed recovery;
an image rollback alone must not silently republish their old text.
