# Existing-account business validation recovery — 2026-10-18

Target: packaging server 172.30.232.42, project `knowhub-company-test` only.

## Evidence and correction

The preceding attempt created one approved binding and one local enterprise
session for the selected original user. This proves those writes occurred; it
does not establish Portal, document access, or complete OIDC acceptance. The
collector saved no complete stage trace and incorrectly treated the login-result
handoff row as another authorization. It then restored protocol-only settings.

Authorization and login-result rows share `oidc_transactions`. Authorization
rows have nonempty nonce; result handoff rows do not. The metadata-only query
now filters `nonce<>''`, without retrieving nonce or result payloads. Genuine
parallel authorizations remain correlation failures. Windows and Linux each
passed 47 collector tests, including the handoff regression.

## Actual runtime recovery

- APP remains `5f988be51779f1a22bd9d027b7503fe6a8749f62`.
- DB remains 3030, dirty=false; no migration or image build.
- Only APP was recreated to load approved business configuration.
- Frontend and other service identities, original user and permission records
  were checked unchanged.
- Exactly one previous active test session and its derived Access records were
  revoked after checking the selected user and approved policy metadata.
  The account, binding, review and audit records were retained.
- Frontend port 80 `/login`: 200. Backend port 8080 `/login`: expected 401.
  OIDC config on both ports: 200.

The APP process matches the approved override: OIDC enabled,
protocol_only=false, contract/email/automatic-binding confirmations enabled,
`verified_email_policy`, `enterprise_all`, `oidc_sso_approved`, and
`local_approved`. Access/absolute/idle/admission periods are 60/60/30/60 minutes.
The actual policy reference is the existing user-conversation approval recorded
in `SOURCEID_FIRST_REAL_BUSINESS_USER_APPROVAL_20261018.md`; it is not represented
as an independent company security officer signature.

JIT remains disabled for this existing-account test. Refresh remains disabled
under the implemented Access-only policy. Prompt login and SID unified logout
remain disabled. Local logout and enterprise session revocation are enabled
through the local-session implementation and do not require SID logout support.
Strict OIDC validation and identity/permission conflict rejection remain intact.

## Observation and pending acceptance

A bounded passive collector uses the reviewed stage, HTTP and gateway metadata
filters. It does not call authorize, exchange a Code, or change business gates.
It preserves sanitized evidence before restoring its temporary gateway logging
configuration. Correlation uncertainty prevents PASS but does not reset
protocol_only while the user is authenticating. The managed automatic-recovery
collector remains available; it is not used for this window.

Run `847f527b-3df8-4b2f-9203-c60c5c03fc21` reached READY. Actual user authentication,
Portal rendering, original-account permissions, document access and logout are
pending. Cross-space negative acceptance is NOT_EXECUTED. No full business PASS
is claimed. No production changes or agent-initiated SID requests occurred.

After the user's result, inspect the new run's stage receipt and selected-session
metadata. Do not infer success from the previous run or a Token 200 alone.
