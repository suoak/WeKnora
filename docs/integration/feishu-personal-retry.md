# Personal Feishu retry and renewed OAuth consent

The history page's existing batch retry endpoint handles two distinct actions:

- Admission failures re-enter the current import preflight, retaining the batch
  and its bounded admission counter. After same-account OAuth consent changes,
  an explicit owner request can update the old batch's credential reference.
  Existing imports must still match the owner, app, account and destination;
  missing import records or concurrent batch changes reject the transition.
  A source-acquisition failure before document creation can re-enter the same
  preflight/Prepare path only after proving there are no knowledge rows, chunks,
  vectors, file bindings, legacy enrollments or publication receipts. Its original
  source-acquisition intent must match exactly. Only the batch admission is queued;
  attempts, counters and grants are not reset by this action. Pending readmission
  is displayed as queued rather than as an unhandled terminal failure.
- Eligible retained personal executions use the existing exact resume protocol:
  original attempt, knowledge, file, receipt and dispatch identities; cumulative
  execution budget; current source/file proof; a new consumer SQL Claim; and
  parser-output comparison before indexing or publication. No archived task is
  replayed and no lease or execution identity is manufactured by delivery.

For an older live-source import, explicit same-account consent can adopt the
current epoch only after source permissions, immutable file/manifest/binding and
the original staging receipt have been checked. The transaction rechecks current
membership, KB write access, app configuration, connection, grant epoch and
publication approval. The old attempt/receipt/dispatch and consent transition are
preserved in the existing audit table. A concurrent revocation or changed
publication rolls back the entire transition.

Workspace-owned exported snapshots retain their existing independent processing
policy: OAuth renewal does not rewrite their original receipt. A failed private
parser row may resume under the same disabled-output and staging proof; enabled,
published, summarized, unrelated or unproved output remains ineligible. Server
format gates, reviewed admission lists, retry budgets and recovery capacity limits
remain enforced. Unsupported media does not become eligible through retry.

Source-managed imports keep the current data-source per-document retry/recovery
protocol. Personal history never releases Hold, changes a source authorization,
resets a cursor or grants publication authority to those documents.

The page offers retry only from the server's eligibility result. Expired/rejected
refresh consent exposes reconnect, and blocked retained output explains that
administrator review is required. Existing polling and APIs are reused.

## Validation

The dedicated CI runs explicit-consent PostgreSQL race tests and original-protocol
regressions. The native acceptance `TestPersonalExactExplicitOwnerRetryNativePublication`
uses `anydoc,sqlite_fts5,personal_exact_live` to exercise parsing, actual PostgreSQL
indexing/publication, retained IDs, duplicate delivery and the full read matrix;
its provider is an isolated fixture.

`TestPersonalRetryCurrentOAuthLive` uses `feishu_live_acceptance` and an explicit
opt-in. Private pre/post-consent snapshots prove that the actual test account's
epoch changed while its app/account stayed identical. Current Feishu view/copy/
export permission reads and original-batch retry run against a disposable clone.
Published document/vector/source digests must remain unchanged. An OAuth update
whose source worker has not yet acknowledged it must still reject derived writes;
the test never fabricates that worker's SyncLog or SQL Claim.

Snapshots and credentials are never committed. Production OAuth interaction is
performed by the account owner; the test itself writes only to disposable databases.
