# Personal MCP timeout and vector retrieval repair

The user-manual knowledge base (`7ee94ea4-f946-40a7-b3d0-8a683323670c`) can exhaust the personal MCP request budget. Two separate fixes address the response and the database work.

- [#170](https://github.com/suoak/WeKnora/pull/170) completes timed-out JSON-RPC tool calls instead of returning an empty HTTP200. The operation budget remains 30 seconds. Its final CI revision passed ten checks; three unrelated conditional checks were skipped.
- [#174](https://github.com/suoak/WeKnora/pull/174) changes ordinary personal MCP vector SQL to authorize canonical chunks in a bulk relation, evaluate source exclusions as statement-local sets, and materialize scalar distances rather than complete vectors. It preserves exact ranking and applies all authorization, current-text, dimension and source checks before LIMIT. Exact published-receipt and workspace-exception paths retain their existing query behavior.

## Read-only production evidence

2026-11-15, Asia/Shanghai. The initial runtime revision was `10b38e66a9825636b486c76fb9552ffac56c11dc`, schema3047/clean. The original incident query and employee Key were unavailable. Diagnostics used read-only database transactions with bounded statement timeouts, an existing vector retained inside SQL, and no employee impersonation. Only counts, plans and durations were returned. No application, index, policy, configuration or business data was changed by these probes.

The KB contained 78 completed/enabled documents, 274142 live chunks (139425 text chunks) and 598449 enabled embedding records. A query with the original canonical/source filters and exact vector ranking exceeded a 25-second diagnostic statement limit. This is a database probe, not a replay of the user's original MCP request.

| Diagnostic | Database execution |
| --- | ---: |
| Vector dimension probes | 2.30s |
| Canonical text candidate join/equality, without ranking | 3.58s |
| Source restriction checks over the coarse index scope | 5.45s |
| Original complete vector diagnostic | exceeded25s |
| Combined bulk/scalar-distance diagnostic | 6.37s |
| SQL rendered from the actual patch | 6.17s, 100 rows |

These are separate measurements with varying cache state and do not add up to an end-to-end request budget. The source restriction plan performed about260939 repeated source lookups. The optimized plan reads each exclusion set within the same SQL statement; it does not cache an authorization decision between requests. Candidates still pass the canonical text reconstruction and live output revalidation. Merely deferring body projection, disabling JIT, or changing join preferences did not produce an accepted improvement and was not added to the application.

## Validation and release boundary

The PostgreSQL equivalence test compares old and new complete ranked rows, scores and text for public/private scopes, multiple limits and thresholds, publication withdrawal, disabled/deleted chunks, inactive tenants, source hold/revocation, private owners, serving receipts, personal-import channels and NULL exclusion rows. It runs against a disposable ParadeDB service in the MCP CI workflow. Existing authenticated HTTP concurrency, cancellation, SSE and permission tests also run.

The performance PR includes the timeout PR until that dependency is merged. Its source was reconciled with main `3dbbe3cceb36bd604f7806bf63337762a6de2da6`; no production deployment has been performed. A faster SQL probe does not establish that all user queries now finish within30s. Confirm complete MCP stage timings and results after a reviewed deployment, including keyword-only, semantic and hybrid requests and concurrent source revocation.

Private evidence lives at `E:/AIData/Codex/manual-search-incident-20261115`: `vector-steps.json`, `guard-steps.json`, `vector-nojit.json`, `vector-bulk-distance.json`, `patch-probe.json`, the SQL renderer and earlier timeout reproduction. Credentials and document bodies are excluded from this report.
