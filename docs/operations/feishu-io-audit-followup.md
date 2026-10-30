# I/O audit extension — independent follow-up design

The current `feishu_headers_io_failure` label alone cannot distinguish Feishu
HTTP from a non-embedding BackgroundModel transport. No request-specific stage,
source or endpoint evidence survived this incident. Retain fail-closed behavior
and first-trip-wins; do not change retry categories while adding diagnosis.

A separate change should carry a server-owned, bounded audit envelope from each
Transport instance into `tripUsingEvidence`:

- `version`: fixed schema version.
- `transport_family`: enum `feishu`, `embedding`, `background_model`.
- `request_phase`: enum `dns`, `connect`, `tls`, `write`, `headers`, `body`,
  `unknown`. Use actual httptrace callbacks where available; a missing response
  alone is not proof of a header-stage failure.
- `error_class`: fixed enum for timeout, EOF, unexpected EOF, reset, broken pipe,
  DNS, TLS certificate, TLS handshake, caller cancellation, unknown I/O.
- `source_digest` and `endpoint_digest`: SHA256/HMAC over canonical source ID
  and scheme/host/port only. No URL path, query, raw hostname, headers or body.
- `first_event_ms`: Redis server time in the same first-trip append; stream ID
  remains the authoritative incident identity.

Never serialize an error string, dynamically formatted type, URL, OAuth token,
request/response body or credential. An absent trace remains `unknown`. Do not
invent historical classifications for event `1793268000119-0`.

Write the envelope only for the first trip using the existing NX semantics;
later controller detections must not overwrite it. Audit failure must retain
local closure/permit removal and drive the existing controller stop. Resource
health failures remain separate from deployment-wide transport failure.

Tests must inject DNS/connect/TLS/write/header/body faults through a local
controlled transport, both with and without httptrace; verify no false stage
claim, no secret/URL leakage, caller cancellation behavior unchanged, first
incident evidence immutable, concurrent trip race, and Redis audit failure.
Test model/embedding/Feishu families separately. Ship in a separate commit/PR
from the recovery capability; it is not a reason to clear the current lock or
a prerequisite to inventing proof that the old network fault was repaired.
