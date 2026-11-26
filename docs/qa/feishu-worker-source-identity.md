# Feishu worker credential context

Automatic datasource workers obtain a database claim and a server-owned source
authority context before resolving the source's OAuth credential. The shared
`batchContext` then checks the current owner and KB permissions using a WebUser
principal. Previously that locally derived principal also reached the outbound
worker gate, which rejects HTTP principals mixed with task execution identities.

The correction is limited to claimed automatic-service workers. It checks the
original live source/generation/runtime guard, rejects incoming principal/key
contexts, and restores only the principal introduced locally for credential
resolution. All owner, OAuth, KB, source identity, execution and publication checks
remain. Ordinary user imports and the generic mixed-identity rejection are unchanged.

Finite transport labels distinguish public safety errors from unclassified
transport failures without changing API categories, error causes or retry rules.
This static conflict does not establish the unique cause of previous production
failures whose underlying errors were not retained.

The isolated tests use a non-system-admin Contributor as the source owner with
write access to their own KB. A separate real database system administrator
configures the test application; no worker receives that administrator identity.
OAuth uses an owner-only private KB. A separate application-grant case uses the
existing public grant mode and a nonprivate KB, obtaining an app token and reading
metadata through the actual guarded client.

The isolated tests use the full migrated disposable PostgreSQL database and an
owned Redis process with a real Asynq consumer/controller. The positive test
directly invokes the actual ProcessSync handler and sends Wiki/metadata requests through the actual
safety/SSRF HTTP client to a synthetic local OAuth/Feishu fixture. The connector
returns no documents: no model performance, real source ACL, Canary publication
or production recovery is claimed. The test file can run against the previous
source without the implementation change, for a failure-before/fix-after comparison.
The fixture's real Asynq consumer supplies actual registration/heartbeats; the
tests do not claim that it delivers the ProcessSync task. Native APP and real
worker delivery acceptance remain separate.

Required Linux execution:

- `go test -tags anydoc -race ./internal/application/service -run '^TestFeishuWorkerIdentity' -count=1 -v`
  with disposable `FEISHU_LEGACY_TEST_DSN` and `FEISHU_SAFETY_REDIS_EXECUTABLE`.
  The application-grant case retains its existing native AnyDoc availability
  requirement; use the qualified Bookworm recipe and `REQUIRE_PRIVATE_POSTGRES=1`.
- Normal source identity, private KB, native publication and automatic-service
  safety regressions; actual native APP qualification remains separate.
- `go test ./internal/datasource/connector/feishu/core -count=1`.

An expired-access-token test runs the actual token provider and OAuth safety/SSRF
wrappers with an in-memory DNS/final-wire fixture. It verifies refresh execution,
persisted rotation, subsequent metadata reads and blocked link-local HTTP before
the inner wire. This does not verify real Feishu credentials or a TLS handshake.

Local status: the complete Feishu core package passed (6.782s). Service tests reached the
Windows native linker, which cannot find the existing DuckDB `rstrtmgr` library;
SQL and service execution are NOT_RUN locally. CGO and native checks were retained.
Linux results must be recorded from the exact reviewed candidate before release.
