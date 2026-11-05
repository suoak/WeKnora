# Normal Feishu synchronization

Normal service uses a separately signed `normal_feishu_sync` authorization with
the `current_authorized_sources_v1` policy. It contains no Source IDs and binds
the installation namespace, verification key and verified gateway allocation.
The administrator consumes its activation signature within 15 minutes. The
service authorization then remains valid until explicitly revoked, independently
of incident recovery windows, worker identity and application revision.

Ordinary employees do not sign policies. For Feishu Wiki/Drive and their Lark
variants, a current space Contributor or higher can create/manage a source in an
editable KB. Other connectors retain their administrator requirement. Cross-space
credential access and another user's private KB are rejected. Personal OAuth
remains bound to its actual owner and the existing consent/publication protocol.

Every manual or scheduled producer automatically registers the current source
binding through `BindApprovedSource`, using the existing `approved_sync` lane.
The registry is an audit/index of automatic admissions, not an administrator
allowlist. The server rechecks the live source configuration, owning space,
writer authority and actual SQL execution generation. Background application
jobs use current space management authority; they do not impersonate an earlier
interactive caller. Connectors still verify remote resource access, and atomic
publication retains current permission, revision, asset and manifest checks.

## One-time activation

1. Set the legacy operational scope to `all`, retain the Safety Guard and its
   verified budgets, and enable the existing safe PostgreSQL ingestion path.
2. Establish healthy controller/consumer state through the existing incident
   recovery procedure if the installation is currently stopped.
3. As a real system administrator, GET
   `/:id/safety-recovery/normal-service/plan`. Review the service boundary and
   independently sign its canonical authorization with `signature` empty.
4. POST the signed authorization to
   `/:id/safety-recovery/normal-service/activate`.
5. Verify newly created Wiki and Drive sources, actual imported chunks/vectors,
   retrieval and later scheduled updates. An HTTP 200 is insufficient.

Activation persists the signed record in `integration.feishu.normal_service`
with history/CAS protection, then atomically publishes the matching Redis record
and opens only `approved_sync`. Neither historical tasks nor source states are
deleted or rewritten. An activation replay does not resume a later operator
pause. The emergency stop remains authoritative.

## Recovery and lifecycle

Historical Hold/revoked sources do not acquire normal read authority. Their
existing separately signed one-shot reconciliation, SQL Claim and public
inventory checks remain required. After verified recovery, normal scheduling
uses the service boundary automatically. Expiry of an incident certificate
closes only that incident's work and cannot invalidate ordinary sources.

A graceful worker restart or deployment reloads the exact durable authorization
and establishes fresh controller membership/permits. It does not require a new
offline signature or rewrite source lists. Queue pauses owned by an operator
remain paused. Unknown worker loss, a missing/tampered durable authorization,
lost Redis control state or a real global safety trip remain closed and require
infrastructure recovery. Changing the verified budget boundary requires a new
service authorization, rather than silently broadening the old grant.

POST `/:id/safety-recovery/normal-service/deactivate` revokes the service, stops
outbound work, and retains the signed authorization/history. An old signature
cannot undo revocation. Revocation and incident controls remain system-admin Web
operations; ordinary source synchronization does not call them.
