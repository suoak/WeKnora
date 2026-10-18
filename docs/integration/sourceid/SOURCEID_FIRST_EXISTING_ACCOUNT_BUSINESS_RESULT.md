# Original-account OA business result — 2026-10-18

Target: packaging test server 172.30.232.42, `knowhub-company-test`.
APP `5f988be51779f1a22bd9d027b7503fe6a8749f62`, DB3030 clean.

## Confirmed by the user

OA login succeeded, permissions appeared normal, and the original knowledge
base and documents could be opened. These are actual interactive observations,
not an inference from Token HTTP200 or a static Portal response.

## Independently checked server metadata

- Active enterprise Session belongs to the selected original user_id.
- Original account remains active and system administrator; one active owner
  membership remains. Cross-tenant flag remains false; no elevation is claimed.
- Three users, one external binding and zero JIT registrations remain.
- One approved link decision targets the selected original account and matches
  the actually recorded user-approved company-email policy reference.
- One active knowledge base and three original document records remain.
- Zero enterprise Refresh records; one active enterprise Access record.
- Three recorded enterprise Sessions, two revoked and one active.
- Actual APP image/revision and health match the deployed candidate.
- Effective business configuration matches the approved override; no gates were
  reset by the observer. No production changes or new migration were performed.

Only aggregate counts and booleans were exported. No employee identity values,
tokens, cookies, authorization codes, document text or full URLs were exported.

## Evidence limits and remaining checks

Passive run `847f527b-3df8-4b2f-9203-c60c5c03fc21` stopped with
`CORRELATION_AMBIGUOUS`; its stage stream does not prove an end-to-end protocol
PASS. The observer's failure did not close business gates or interrupt login.
Further collector investigation must not require another real login without
authorization, and must not be confused with the user's successful business use.

Current outcome: original-account interactive positive path CONFIRMED_BY_USER,
with account/session metadata corroboration. Full automated acceptance remains
INCOMPLETE. Complete before/after ACL equality, vector counts and HTTP-stage
results are not reasserted by this read-only check.

The user subsequently confirmed local logout. A fresh read-only database check
then showed zero active enterprise Sessions, all three recorded Sessions revoked,
and zero active enterprise Access records. Enterprise Refresh records remain zero.
Original account, binding, approved policy, owner membership and administrator
flag remain present. Local logout and stored Session/Access revocation are thus
corroborated; no replay of a real Access token was performed.

APP remains healthy, DB3030 remains clean, and approved business configuration
remains loaded. Session expiry, cross-space negative authorization, JIT and SID
unified logout were NOT_EXECUTED in this result. The observation limitation above
still prevents claiming complete automatic protocol-stage acceptance.
