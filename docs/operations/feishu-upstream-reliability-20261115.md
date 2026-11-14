# Feishu reliability: upstream comparison and bounded downloads

Compared Tencent/WeKnora `upstream/main` at `cbdcebabf` with the deployed
KnowHub main `6c1b7a19`. This change adapts the following upstream fixes while
retaining the fork's source authority, generation, publication and stop fences:

- `662abaf9f` / upstream #4736: count unchanged nodes toward checkpoint cadence.
  Here, a failed checkpoint stops traversal immediately; the upstream warning-only
  behavior is not adopted. Explicit document deferrals retain their existing
  checkpoint/authorization policy without accepting a new document version.
- `184cec1ac` / upstream #4738: skip Finder/Explorer/Office bookkeeping files.
  This is restricted to uploaded Drive files. Native documents with similar
  names still follow normal content and access checks. Ignored files remain
  visible as unsupported/skipped work; they are not reported as imported.

## Binary transfer budget

Production diagnostics after #160 showed a 212505652-byte response returning
approximately 31–35 MB before the shared metadata client's 30-second deadline.
HTTP 200 headers therefore did not establish that the source had downloaded.
The existing empty-response fix correctly classified failures and allowed ten
other documents to publish in one sync generation. Permissions and large-file
transfer reliability remain separate issues.

Binary operations now use a ten-minute total budget, including auth, pacing,
retries and backoff. A thirty-second progress timer bounds initial headers and
every gap between received bytes. Receiving bytes renews only this idle timer,
never the total budget. Metadata keeps its existing thirty-second client
timeout. A per-call client copy shares the existing guarded transport, redirect
policy and connection pool without mutating the metadata client.

An idle deadline cancels the same request through its context. A total download
deadline is reported as a transient document timeout; cancellation of the
parent sync remains parent cancellation. Failed reads yield no source bytes.
The 512 MB size cap, empty-file refusal, request retries, credential handling,
SSRF protection, source verification and publication receipt checks remain.

Regression coverage includes slow but progressing downloads, unchanged metadata
deadlines, stalled headers, partial bodies, indefinitely dripping bodies, caller
cancellation, source isolation, unchanged-node checkpoints and durable write
failure stopping before the next resource. Full native connector/safety tests,
focused race/vet and the repository CI gate are required before rollout.

## Release scope

This is a targeted adaptation, not a full upstream merge. It does not alter
source permissions or claim that a permission-denied or provider-empty document
was imported. Wiki ingest records and page finalizer operations must be counted
separately when reviewing progress. Wait for active Wiki model work to finish
before the consolidated backend replacement; then verify real incremental sync,
published source receipts and authorized document/Wiki reads.
