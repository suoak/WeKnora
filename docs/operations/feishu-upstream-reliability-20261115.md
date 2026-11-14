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
- `b373374bf` / upstream #4400: retain nested lists/callouts inside native
  table cells in source order. Iterative traversal handles repeated/cyclic
  child references, while file/image subtrees keep their existing flat rendering.

The broader upstream Wiki-shortcut expansion (#4565) is not imported here:
cross-space descendants need this fork's selected-source authority and durable
discovery review, not an automatic widening of the current selection. Upstream
error-preview truncation (#4614) changes a helper with no call sites in this
fork's Feishu core; current errors retain structured codes, not provider bodies.

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

## Typed native resource refusals

Live validation completed the previously interrupted 212505652-byte download.
The next boundary was the native converter's typed `resource_limit` refusal,
which the old service treated as an unknown admission error and retried as a
whole sync. This repeats the large download and earlier permission failures.

The cgo adapter now preserves a stable `ErrResourceLimit` marker only when the
original native `ConvertError.Kind` is exactly `resource_limit`. Wrapped causes
remain inspectable; matching words in ordinary errors do not set the marker.
The legacy Feishu safe-incremental path records
`safe_staging_parser_resource_limit`, leaves the failed candidate unpublished
and its version unacknowledged, then continues other documents. Resource limits
are retained. A resource refusal during image extraction also rejects the
candidate rather than publishing a text-only fallback. Unknown parser errors,
unavailable engines, source authority failures and durable-write errors retain
their existing handling. Resource-limited files need source repair or splitting;
they must not be reported as successfully imported.
