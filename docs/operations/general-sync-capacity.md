# Reserved capacity for general datasource synchronization

Feishu safety stops and durable recovery are independent from queue scheduling.
Even when every queue is enabled, long Feishu tasks can occupy all maintenance
workers and prevent existing GitLab tasks in `sync` from starting. Queue weights
cannot preempt a running task.

The `general_sync` queue and pool reserve one additional per-instance slot for
non-Feishu sources. Manual and scheduled producers use the persisted connector
type to select it. The normal maintenance pool still serves `sync`, `approved_sync` and `low`; parsing,
enrichment, Wiki and cleanup retain their existing guarantees. The default
upstream worker count increases from 32 to 33 and runtime reporting includes the
extra pool and its actual heartbeat capacity. Existing model and connector
limits remain in force.

The dedicated server tags execution through a private context key. Before
claiming SQL execution, the datasource service loads the persisted source and
checks its tenant and connector type. Feishu/Lark Wiki and Drive deliveries on
this general-only lane are rejected before SQL execution. A second check before connector
execution covers a changed source. No task body or HTTP field can request this
worker identity. The reserved server does not subscribe to `approved_sync`.

Approved-source consumer attestation remains unchanged. The dedicated server
never consumes the old `sync` or approved queue and cannot attest their liveness.

The existing minute reconciliation loop inspects at most 100 native pending
`sync` deliveries and respects an explicitly paused old queue. It relays only
original first attempts with no prior failure, grouping, SQL generation or
claimed execution. It verifies source tenant/type/state and the original running
SQL log, preserving payload bytes, headers, native ID, timeout, deadline,
retention and maximum retries on the new queue. The original delivery remains
untouched. Stable IDs coalesce duplicate relays; the original SQL run's atomic
generation claim allows one execution and makes a later old delivery a harmless
duplicate. The live-task inspector scans both lanes. Previously attempted
deliveries cannot receive a fresh budget.

Validation includes a real Asynq saturation test with four occupied Feishu
workers, an old zero-budget Feishu delivery in `sync`, and an ordinary delivery
on the reserved queue. The ordinary delivery finishes while the four tasks remain
blocked, and the old Feishu delivery retains its bytes and zero retry count.
Asynq 0.26 checks retry exhaustion before IsFailure, so a mixed-queue worker
rejecting old Feishu tasks with a soft error would archive exhausted deliveries;
the new lane avoids that behavior. SQLite/PostgreSQL tests
exercise the real datasource service and SQL generation claim, duplicate
delivery handling, and rejection of all four Feishu/Lark connector variants
before claiming, first-attempt relay admission and execution once across both
deliveries. The shared-sync CI gate also runs the saturation test with its
explicitly disposable real Redis service.

Release this with the next consolidated deployment. Preserve enabled queues and
normal-service activation; do not pause Feishu to create general-sync capacity.
After release verify queued original GitLab run IDs execute and publish through
the normal generation-fenced path, and verify the additional runtime heartbeat.
