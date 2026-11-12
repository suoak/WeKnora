# Personal MCP content creation — development receipt

Status: development candidate; not deployed or accepted for production.

## Baselines and boundaries

The worktree is `feat/personal-mcp-content-write-20261112`, based on fetched
`origin/main`. The original SourceID worktree and its uncommitted files are
untouched. Upstream `internal/mcpserver/tools_ingest.go` was compared: its text
creation service is reusable, but its endpoint grant and URL import are not a
Personal Key authorization boundary.

The production snapshot in `deploy/knowhub/production-20261112/README.md`
records APP `dc3f11db077a2eb6aef8abf1b8e3616d3f7a3377`, schema 3046. This is
repository evidence, not a fresh observation of the running service. This change
adds no migration and does not authorize upgrading production to main's schema.
Operators must validate the candidate against the deployed schema before release.

The user confirmed that the isolated environment is mapped by operators and is
not accessible for deployment here. Its public URL remains
`https://knowhub.ruijie.com.cn:8443/api/v1/mcp/personal`. The checked-in Nginx
mapping forwards it to `/internal/mcp/isolated/personal`. That handler retains
the original eight-tool registry and protocol boundary. The external handler
has a separate registry and two content admission slots; query slots remain
eight. No request header selects the registry.

## Authorization findings and implementation

`ResolveKB` assigns Admin reach for same-space KBs. `RequireKBWrite` consumes
that grant, but alone does not implement the web ownership rule. Web upload
routes additionally use `OwnedKBOrAdmin`. Personal Keys bypass the ordinary
machine-key ownership middleware, so reusing that route stack is insufficient.

`CheckKBContentCreate` checks server-loaded KB state, private ownership,
same-space creator/admin authority, or the existing capped cross-space Editor
share. Both manual and file creation services consume this additional admission
for authenticated callers. Trusted imports without a user or Key retain their
existing internal invocation contract. No membership or sharing is created.

The existing web ownership lookup treats foreign-tenant resources as not found,
then lets `KBAccessWrite` resolve organization sharing. Consequently a current
Editor share can pass the existing web upload gates; Viewer and shared-Agent
read access cannot. The Personal implementation uses the same share resolver.

`add_content` is an explicit, default-off capability stored in the existing JSON
capabilities column. It is accepted by personal Key management but is not an
`ingest` REST capability. `list_writable_knowledge_bases` independently loads KB
metadata and checks the current Key, membership, scope, private owner and share.
Execution repeats those checks. Fixed Keys use persisted scope entries, including
share-row pinning; dynamic Keys use current memberships plus actual ownership or
share permissions. Public search grants are never used for admission.

Current account eligibility reuses `KnowHubPersonalUserVerifier`, which checks
active accounts, not external real-time employment. If employee termination is
not reflected immediately in account/membership lifecycle, an independently
configured current-employee verifier is still required before acceptance.

## Text delivery and idempotency

External `add_document` accepts exact KB ID, title, content, publish (default
true), and mandatory `idempotency_key` (8–128 bytes). Content is bounded to
512 KiB and the request to 1 MiB. It uses `CreateKnowledgeFromManual` and the
existing asynchronous parsing/indexing pipeline. It returns document ID, parse
status and failure reason. No update/delete/configuration tools are added.

The credential and idempotency key determine a stable UUID. An internal request
fingerprint persists in manual metadata. Primary-key uniqueness prevents
concurrent inserts across replicas. Replays reauthorize before reading only a
receipt projection; a different payload conflicts. A failed queue submission
returns the existing failed document, rather than creating another document.
Existing task retries and operator recovery remain responsible for processing
failures. Deleted document IDs remain reserved by primary-key uniqueness.

URL/path import, embedded images and stored-resource references are rejected.
Audits contain actor, Key ID, KB/document identifiers, outcome and timestamp;
the content-specific audit excludes secret, title and body.

## File upload: explicit subsequent delivery scope

PDF/DOCX/XLSX upload is not implemented in this candidate. It remains required
for the overall feature and must not be advertised in supported_content_types.
Deliver a bounded `upload_document` or upload-ticket flow that receives bytes,
never client filesystem paths or remote URLs. Reuse the same live admission,
receipt/idempotency design and existing `CreateKnowledgeFromFile` pipeline.

Before enabling it: enforce decoded size and multipart/base64 overhead, extension
and magic/MIME agreement, OOXML archive expansion limits, reject macros and
encrypted/unscannable content, scan quarantined bytes with a configured scanner
(failure closed), allocate server-generated temporary names with quotas and TTL,
clean up on success/failure/cancellation, bind tickets to owner/Key/KB/hash and
expiry, and recheck authorization when consuming each ticket. Parsing must use
the existing workers and must not fetch embedded network resources. This scope
requires its own synthetic upload/scan/parse/retry acceptance evidence.

## Acceptance, compatibility and rollback

Local tests added: ownership/share matrix, separate tool registries and slots,
isolated write/header denial, and real SQL users A/B with independent KBs,
concurrent idempotent replay, conflict, membership and capability revocation.
Execution results must be recorded separately; test presence is not a pass.

Local checks on 2026-11-12: frontend `vue-tsc --noEmit` passed; the personal
presentation/lifecycle test suites passed 13/13 tests; `git diff --check` passed.
The checked-in 8443 proxy mapping was verified to pin the isolated backend path.
Initial Go runs hit host paging-file exhaustion, then the existing Windows
`pg_query_go` stack-protector linker dependency; retries use `GOMAXPROCS=2`,
`-p 1` and `CGO_LDFLAGS=-lssp`. The complete internal/application/access and internal/mcpserver test suites
passed. Service integration could not execute: after adding the installed SDK rstrtmgr
import library to the local linker search path, DuckDB static libraries still
failed with C++ runtime symbols (_Unwind_Resume / __gxx_personality_seh0).
The real SQL fixture is present but NOT PASSED. Run it on a supported Linux
build/test runner; do not treat successful compilation of Go sources as
database acceptance. Handler package selected-run compilation passed (no
matching handler tests). No production/deployed MCP call was made. These are local process settings, not application/deployment changes.

Before merge, operators must run all thirteen requested scenarios with actual
authenticated MCP calls, including both external and 8443 with the same Key,
publication/indexing/search, current-schema compatibility, revoked share rows,
and file upload when delivered. This workspace has no authority/access to
complete that environment acceptance. Do not merge or release on mock results.

Rollback: revert candidate application/frontend images through the current
approved Compose chain; preserve the 8443 proxy mapping, certificates and
no-Docker-socket overlay. Existing Keys retain their old capabilities. Reverting
code makes `add_content` inert; do not remove created knowledge or alter user
permissions. No database rollback or schema upgrade is part of this change.
