# Personal MCP fixed capabilities

Personal (`user_mcp`) credentials include `retrieve` and `add_content` on creation,
replacement, partial updates and secret rotation. Optional capabilities remain
unchanged unless explicitly edited. Rotation locks the current row and updates
the capabilities together with the compare-and-swap secret change. Machine API
keys and FullAccess are not expanded.

Capability normalization occurs only on persistence, never implicitly during
authentication. Existing valid credentials require an audited, idempotent repair
only if a mandatory capability is actually missing. Revoked or expired keys and
inactive accounts are excluded. The last production read-only inventory found
80 valid Personal keys with neither mandatory capability missing.

Content writes retain the existing live user, fixed-range and knowledge-base
content-create authorization. Public search, Viewer access and space membership
do not confer write access. The isolated TLS endpoint retains eight read-only
tools. External discovery expects twelve tools; the browser connection test now
checks the correct set for the selected endpoint.

The connection dialog removes the content opt-in switch and long introductory
alerts. Detailed file guidance is collapsed under connection help. The isolated
endpoint is labeled `隔离· 仅查询`.

Validation before PR: Linux key lifecycle and repository tests, complete MCP and
content access packages, native application build, frontend type check and 29
focused frontend tests passed. Browser and CI results are recorded in the release
evidence separately. External employee status integration and ClamAV remain
deferred by product decision; existing local-account checks and file limits stay.

No migration is introduced. Production schema remains 3047; automatic migration
must remain disabled. Build paired application/frontend artifacts from the merged
main SHA, preserve the effective production Compose chain and security mounts,
and retain the preceding images and private runtime configuration for rollback.
Application rollback does not reset the database or manipulate sync queues.
