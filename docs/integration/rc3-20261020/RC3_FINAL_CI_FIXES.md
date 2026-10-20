# RC3 final CI repair — candidate verification in progress

This report supersedes the known failures in RC3_MAIN_CI_REVIEW only where new evidence below exists. Main has not been merged; production is unchanged. Current main: `854f3b956ae3600e12a3ffd2fc40d4fe624ced74`.

## OA first-binding concurrency

The losing login could miss the approved review SELECT, then see an enrollment committed by a winning first login. The pilot path rejected immediately, although the matching approved review now existed. It did not previously bypass review; it caused a legitimate concurrent login to fail.

The fix re-reads and locks the approved review, rechecks issuer/RJGH/sub/expected user, review provenance and approved account ownership, then locks/rechecks enrollment and resolves the existing identity inside one transaction. Revocation, wrong identity, wrong user, inactive account and unreviewed enrollment remain rejected. Session creation retains its existing independent review checks. No authentication strategy or JIT defaults change.

A channel barrier pauses the losing request after its real missing-review read. Only then does the winning request commit. The old implementation deterministically fails the approved case. Tests do not prebind the account before the losing read. Real PostgreSQL constraints remain enabled; the malformed unreviewed-record scenario explicitly checks the CHECK constraint and separately injects a corrupted read.

## Cross-importer application-grant contract

The product route accepted a same-space contributor with canonical KB write permission; PersonalRecovery redundantly required that user to be KB creator/admin. That extra restriction rejected the request before deduplication. The handler then mapped access.ErrForbidden to generic 400. This was neither a unique-constraint collision nor reuse of another employee's OAuth credential.

Use the existing live membership and canonical KB editor/write checks. For the same application/resource/KB, another importer receives `400 feishu_already_imported`, without the original import record, attempt, owner or KID. Same-user retry remains idempotent; a separate authorized KB can have its own import. Access denials are checked first and mapped to typed 401/403/404 without internal detail. No user_oauth opening or changed-version replace is introduced.

## CI cleanup scope

The real unlimited main-diff lint run initially exposed 1,705 diagnostics, not merely the 174 displayed by the capped GitHub log. Cleanup touches 181 code/test files, mostly line wrapping, formatting, exported comments and explicit ignored cleanup/fixture response-write errors. Audit rules and baselines are unchanged. String concatenation preserves literal bytes. Existing deprecated TLS override detection remains in place with an explanatory local staticcheck directive; it is not removed to satisfy lint. The unused workbook-media validator and its unused helper are removed; the active workbook/attachment validation path remains.

One pre-existing ignored knowledge-state persistence failure in the DocReader-unavailable branch now propagates instead of pretending the failure was saved. Other best-effort cleanup error dispositions retain their prior behavior. No new migration, no 3032, no queue cleanup, and no index replacement are added.

Frontend uses existing 12px/150ms design tokens. The i18n audit resolves each imported local useI18n bundle and requires local keys in all five locales; unknown bundles fail closed. It no longer incorrectly treats valid local Feishu keys as missing global keys. No baseline increase or missing-key assertion removal.

## Actual isolated results

| Gate | PASS | FAIL | SKIP | Evidence |
|---|---:|---:|---:|---|
| Linux full Go, native anydoc, real isolated PostgreSQL | 14023 | 0 | 80 | artifacts/full-go.jsonl, full-go.exit=0 |
| Go vet | — | 0 | — | artifacts/vet.exit=0 |
| Go lint, v2.12.2, unlimited actual main diff | — | 0 | — | artifacts/lint-pass.json: 0 issues, exit=0 |
| Deterministic OA SQLite + PostgreSQL Race, count=5 | 150 | 0 | 0 | artifacts/oa-deterministic.jsonl |
| Parallel first login Race, count=100 | 100 | 0 | 0 | artifacts/oa-parallel.jsonl |
| Cross-importer and handler error contracts Race | 8 | 0 | 0 | artifacts/duplicate-final.jsonl, focused3.exit |
| Focused frontend quality/i18n | 16 | 0 | 0 | external frontend focused logs |
| Full frontend on Windows | 1575 | 2 | 1 | two POSIX shell execution failures; not waived as CI PASS |
| Frontend type-check | — | 0 | — | external type-check exit=0 |

Go counts include subtests and repeated runs; they are not counts of unique test definitions. Skip remains skip. Linux DNS preflight resolved open.feishu.cn to three public addresses, without hosts entries, pinned IPs or weakened SSRF checks. The historical ::1 resolver anomaly is not attributed to SID or a proven external DNS cause. The same public-HTTPS test now passes in the complete Linux run.

## Full PR scope and release gates

`evidence/final-full-pr-audit.json` inventories every original 954 PR path, its history and candidate hash, plus four added code/test paths. All 3024–3031 and 3033 PostgreSQL/SQLite SQL hashes remain equal to the previously validated RC3. The inventory is not a claim of a fresh manual line-by-line review of every historical document. Security-sensitive OA, import authorization and migration boundaries were reviewed separately.

PENDING: final committed-source APP/UI build, updated test deployment, real ordinary-user OA/ACL/logout and three-format product smoke, GitHub final Linux frontend and workflow CI. Draft PR stays Draft until these gates pass. NOT_EXECUTED is never converted to PASS.

Production additionally needs the maintenance window, named owners/emergency contact, maximum accepted legacy-source staleness and escalation, explicit HTTP OA risk acceptance, approved production time_limited policy, external auto-update confirmation and formal release/rollback approval. Test continuous_test configuration cannot be copied to production. Python MCP remains preserved; its real-call gate is waived by business decision, not passed.
