# Workspace knowledge visibility incident — 2026-10-23

## Cause

Source synchronization uncertainty was enforced at knowledge-base scope, including ordinary authenticated workspace browsing. Catalog fan-out silently omitted source-restricted KBs. The browser rendered a failed KB detail load as an empty library, making intact data appear deleted.

The first hotfix restored catalog metadata only. This was insufficient: detail and document requests still failed. A subsequent end-to-end check also found that the response writer retained the context captured before authentication. It therefore rejected output after downstream code had correctly authorized the workspace member. These incomplete validation boundaries caused repeated visible failures.

## Production evidence

- Workspace 10019, CSBU: 266 persisted, nondeleted documents; source awaiting verification. Detail initially returned 503 despite a visible card.
- Workspace 10005, 国际安全概念1: 115 persisted, nondeleted documents. A held staging resource referred to a document ID absent from the document table; none of the 115 existing documents matched that resource. Scheduled source synchronization failed on 2026-10-23 at approximately 12:03 China time.
- The source hold and unfinished import are not evidence of knowledge-base deletion.

## Changes

1. Keep metadata visible in the authenticated owning workspace's catalog without exposing generated content from a restricted source.
2. Separate an owning workspace member's existing local browsing from a source synchronization hold. API keys, MCP, cross-workspace access, actual source revocation and quarantine of persisted documents retain their separate checks.
3. Revalidate output against the current authenticated request context rather than the context captured before authentication.
4. An unfinished staging import without a persisted document cannot block unrelated existing workspace documents. Its own hold is retained.

No schema migration, document restoration or direct source-state rewrite is required.

## Validation and release discipline

Regression tests cover catalog visibility, local document reads, cross-workspace isolation, strict API/MCP behavior, actual source revocation, persisted-document quarantine, unfinished imports and authenticated response output. Linux APP builds and production checks include KB detail, paginated document totals, document detail and nonempty stored text chunks. Failed deployment validation automatically restores the previous APP image.

Future changes to source protection must validate this full browser path, distinguish request failure from an empty result, and be merged into main so deployment behavior survives subsequent releases. The current frontend error-as-empty behavior is documented here; this backend incident fix does not replace that broader UI error-state work.
