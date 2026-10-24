# Feishu request cancellation and production recovery follow-up

Production `main` revision `6e168beac29ee6ca7aa1a549e0428055ab8a52c2`
was deployed on 2026-10-24. APP, frontend, SourceID configuration, workspace
10019's two knowledge bases and existing document listing passed acceptance.
An authorized MCP query in workspace 10005 returned three in-scope results;
its temporary endpoint was removed. The first incremental source run succeeded
without changes. Two additional incremental runs succeeded without changes.
These are incremental checks, not evidence that every existing file was re-read.

Recovery of the other five eligible sources triggered the shared
`outbound_response_failure` latch at 00:53:59 UTC. The controller removed the
permit, paused only sync and fenced the executions; APP and low remained up.
No latch was cleared. Explicitly paused/held sources were not enabled. Existing
version conflicts and `source_identity_legacy_archive_unavailable` were also
observed and remain protected. This patch does not relax those version checks.

## Confirmed defect and evidential limit

`internal/feishusafety/transport.go` previously treated every non-EOF body error
and every RoundTrip error as a cluster-wide failure unless the entire worker was
draining. A caller cancelling one task/source is a different case. Cancellation
can follow a source claim/version failure or explicit task cancellation.

Four regression cases, source/embedding before headers and during response
reading, failed against the deployed implementation: one caller's cancellation
latched the guard for all sources. All four pass with this patch. Real local HTTP
tests cover cancellation and truncated Content-Length responses.

The production latch did not record error class or request family. Consequently,
the production event alone does **not** establish that caller cancellation was
its first cause. It may instead have been a real timeout or truncated response.
Do not clear that latch or report this event as definitively explained.

## Minimal change

Retain the original caller context separately from the transport's own cleanup
context. Suppress a global trip only when **both** the returned error and original
caller context establish `context.Canceled`. Release the request reservation and
return the cancellation normally. A deadline, unexpected EOF, generic I/O error,
or cancellation without matching caller evidence still closes the guard.

Trip reasons now use bounded content-free family/phase/category values, such as
`feishu_response_timeout`, `embedding_response_unexpected_eof`, or
`feishu_response_canceled_without_caller`. No URL, error text, credentials or
document contents enter those reasons. First-cause latching remains unchanged.

No schema, source status, cursor, permissions, atomic publication, old-version
retention, Redis failure policy, controller membership or budget-limit changes.

## Confirmed local budget saturation

The same audit found a separate deterministic escalation: `Guard.begin` changed
the existing `ErrBudgetBusy` category into `ErrClosed` when its bounded wait
expired. Both reservation and direct transport then latched the cluster. Under a
small TPM budget and several maintenance jobs this can stop otherwise healthy
sync simply because another job consumed the allocation.

Six baseline cases (in-flight/RPM/TPM, reservation/direct transport) reproduce the
wrong error; coverage also includes RPS. After the wait, recheck the live shared
gate, retain `ErrBudgetBusy` only for known saturation, and return without a wire
request or global trip. Redis/permit errors retain the closed path. Model-level
coverage verifies nil vectors, zero HTTP calls and no protocol-level escalation.
No quota is enlarged, no counters are refunded and no budget is bypassed. A
failed staging attempt still returns an error and cannot publish partial content;
existing task retry/backoff handles the local refusal.

## Validation and release

- Baseline: all four new cancellation cases fail; deadline cases stay closed.
- Windows isolated package: full safety package passes; repeated package run passes.
- All new test names include `Safety`, so the existing integrated real Redis and
  combined race selections execute them without changing CI filtering.
- Before merge/deploy: exact-SHA integrated gate, full backend, lint, race and
  App/Portal checks; isolated real HTTP/Redis and final image Startup Smoke.
- Preserve the first production latch/audit and original backup/image evidence.
  A subsequent version must drain prior workers and start frozen before a
  controlled recovery. A recurring genuine I/O failure remains a stop condition.

Historical same-revision content conflicts without a verifiable old archive
remain unresolved. Do not clear high-water records, delete tasks, reset cursors,
or manufacture a new source revision to bypass them.
