# Feishu long-document recovery (2026-10-24)

Baseline main/production: 244041904eaf9241c4efc44712d1a3f1d673ea9b (PR #44).
No schema migration or datasource/KB configuration change is required.

## Confirmed production defects

1. A single uploaded XLSX in CSBU quality failed with a classified transient
   `download_drive_file` timeout (HTTP 200 body). The stream adapter still
   returned a fatal admission error, causing the whole source to retry and
   revisit previously failed documents. Feishu HTTP retries remain finite.
2. The projects R16 PM spreadsheet parsed successfully in 12.68 seconds at
   19:03:55 CST, then remained in embedding after its 19:23:42 candidate lease
   expired. Safe staging allowed a two-hour Asynq task but issued a fixed
   twenty-minute candidate lease without renewal. Publication would reject it,
   leaving the expensive model work unable to publish. The expired generation-1
   execution was canceled through Asynq CancelProcessing; no task was deleted,
   and the old knowledge/config/cursor were retained.

## Minimal changes

- Renew the live candidate before and after each bounded staging embedding
  batch. Reuse `owned` source/tenant/claim/nonce/generation/lease checks in a
  transaction; update only lease_until. Expired owners cannot be revived.
  Renewal never shortens a valid lease, changes a watermark, changes a model
  or disables any publication check. DB renewal calls have a bounded timeout.
  A failed renewal stops further batches and returns no partial vectors.
- The shared Wiki/Drive engine marks primary document fetch failures with
  `failure_stage=fetch_document`. Only classified transient/rate-limit failures
  from that stage can return an already-recorded document failure. Preserve its
  prior accepted edit time, persist exact-identity pending retry state, and
  continue other documents. Partial listings, unknown errors, auth/permission,
  absence, cancellation, claim and lease errors retain their fatal behavior.
- Keep failed, deferred, partial and success distinct. The four existing
  explicitly authorized deferrals are unchanged. A failed fetch is not a
  deferral or successful publication, and a pending retry overrides the
  unchanged-time shortcut after restart.

No new general retry loop or service. The existing finite HTTP retries,
controller, conservative shared limits, SSRF protection, task timeout and
atomic publication remain in force. Ordinary online models are not wrapped.

## Isolated validation

Linux race / real PostgreSQL / real parser test v2: 1,116 JSON test events,
exit 0. Covers live renewal, concurrent renewals, unchanged resource ownership,
real DB deadline expiry, no revival after expiry, wrong nonce/generation,
lost claim, paused/revoked/canceled execution and stopping further model calls.
Existing real parser, parent graphs, vector/BM25 and old-version protection
pass with the renewal wrapper wired into staging. Document transient fetch,
checkpoint and restart tests prove later documents continue and retry markers
clear only after acceptance. Final-commit CI remains required before release.

An independent configuration-only test on the baseline main proved a shared
120,000/minute UTF-8 input upper-bound budget with in-flight=1, RPS=1, RPM=30:
two fresh controlled Feishu documents published 177 child vectors without gaps,
and real retrieval returned five matches. Production was still at 30,000 when
this evidence was captured. This is an application allocation, not a claim of
exclusive upstream quota or verified account remaining capacity.

## Release and limits

Use the verified frozen rollout, schema-3038 no-op check, protected source and
version binding comparison, old-consumer drain and immutable-image smoke.
Retain old images, config, source archives and backups. Resume eligible sources
only after the new controller/guard policy fingerprints match all consumers;
never change explicit paused/hold/revoked states. Native file range downloading
and versioned image ingestion are not implemented by this patch.

This does not repair already conflicting historical content observations,
recover a missing original candidate archive or revive an expired lease.
Trustworthy source/archive evidence or an actual new source version is still
required for those resources. Huge graphs exceeding the existing atomic graph
cap remain explicit failures. Old sources/backups stay retained pending full
completeness acceptance; October 27 19:25 CST extension requires owner approval.
