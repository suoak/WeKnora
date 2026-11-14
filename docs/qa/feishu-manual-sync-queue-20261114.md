# Accept manual sync requests while workers recover

The user repeatedly received a service-stopped error immediately after an APP
replacement, although the controller had a valid permit and normal service was
active. Production admission logs identify Redis `NOAUTH` on the source pool.
Connection initialization inherited a controller-marked context and the routing
hook sent AUTH/HELLO/SELECT to another pool, leaving the original connection
unauthenticated. A password-protected Redis regression reproduces the failure
before the fix and passes afterward, including non-default database selection.
Keep connection initialization commands and pipelines on their sticky connection.

Manual normal-service admission now checks durable signed service authorization
and current source access, then persists/enqueues a pending request without
requiring a live runtime permit. Controller pauses delay consumption; workers
still rebind current source authority, permits and execution generation before
reading the source or publishing anything. Queue acceptance never clears a trip,
resumes an operator pause or recreates a service authorization. Explicitly revoked
service/source authority, paused/held sources and invalid callers remain rejected.
The existing UI receives its usual accepted sync response instead of a platform
recovery error. The logical run remains running while queued or retrying.

Concurrent manual clicks return the existing running task when the atomic DB
admission loses the race. Twenty callers must observe one task and one enqueue.
Actual queue/storage failures remain errors: requests are not falsely reported
accepted if persistence fails.

Validation: protected-Redis before/after regression, controller deadline/pool
isolation tests, waiting without outbound authority, revoked-source/service
rejection, and SQLite/PostgreSQL concurrency tests. No migrations, credentials,
model settings, source content or DocReader changes. Required CI and merged-main
startup smoke precede APP-only deployment.
