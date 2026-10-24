# Confirmed Feishu source timeout isolation

Final main 75e4bcc6 was deployed and healthy on 2026-10-24. Its shared
controller accepted one current maintenance consumer, restored nine total cron
entries and enabled the sync queue. Three production incremental source runs
succeeded, and authorized MCP retrieval returned three correctly scoped results.
Five other runs were fenced after the first-cause latch recorded
`feishu_response_timeout`. The permit was removed, active/in-flight work drained
and sync paused; online APP and low remained available. This is confirmed source
HTTP timeout evidence, distinct from an Embedding gateway error. The initiating
source/operation was not recorded by that bounded reason and remains unconfirmed.

The Feishu client already has per-request deadlines, a shared per-application
request limiter/semaphore, typed transient errors and finite exponential retries.
The safety transport tripped globally on the first body/header timeout, so those
existing retries could not recover and unrelated sources were canceled.

This patch lets **only source HTTP timeouts** return to that existing retry
mechanism after rechecking the shared guard with an independent bounded context.
No response is accepted after a timeout. Retry exhaustion fails that source's
task and staging cannot publish partial knowledge. Task cancellation is still
local, and normal backpressure remains bounded and local.

Embedding timeouts, 429 exhaustion, HTTP quota/server failures, truncated or
otherwise corrupt responses retain their existing global-stop policies. Missing
permits, Redis failures and unconfirmed control state still close the gate,
including when they coincide with a source timeout. No limits, SSRF, claim,
generation, version ownership, atomic publication or old-knowledge protection
are relaxed. No source settings, cursor, pause/revocation or hold are changed.

Verification includes source/embedding header and body deadline policy, missing
permit/Redis failure, and real local HTTP client body timeouts that recover or
exhaust exactly three fixture attempts while an independent source remains live.
The core production retry count and delays are reused without modification.
The existing client timeout is also expressed as each attempt's context
deadline. Go's HTTP client does not recognize the wrapped transport as a built-in
transport and can otherwise report a legacy body cancellation before wrapping it
as a timeout. Explicit attempt deadlines preserve the same timeout duration and
parent cancellation while allowing reliable classification. Attempt contexts
are canceled after every error or response-body close. API error presentation
adds only the fixed `cause=timeout` marker and never includes the underlying
error string, response body or credentials.
All added tests contain `Safety` for the integrated real Redis/race selection.
Windows safety tests pass; full connector tests use Linux because the local
MinGW pg_query link dependency lacks `__stack_chk_fail`.

The original production latches and audit remain preserved. This fix does not
resolve historical source content conflicts or missing archives, and does not
enable the two paused or two held sources. Exact candidate/main CI, isolated real
sync/image smoke and immutable image verification are required before upgrading.
