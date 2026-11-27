# Classic DOC IRM incident, 2026-11-27

## Read-only production evidence

The user selected the active 19:13:44 (Asia/Shanghai) `suchen1` upload of
`NTOS-UDB-HLD.doc` for repair. The earlier 17:25:26 record was already soft
deleted at 17:30:39, with no remaining original at its registered path. This
investigation did not delete it. Do not claim that its bytes were recovered.

The active original is 3,358,208 bytes; SHA-256:
`0d42f9997bbb6a3eb21d5e2e6081c566d5024e574a7992d8b1d27cb363c05c6a`.
It was downloaded to a private directory outside the repository and inspected
read-only. No production document, license stream, credentials or content is
included in this report or in fixtures.

Relevant live CFB paths (control characters escaped):

| Path | Kind | Bytes |
| --- | --- | ---: |
| `WordDocument` | Stream | 4096 |
| `1Table` | Stream | 7093 |
| `\x09DRMContent` | Stream | 3198136 |
| `\x06DataSpaces` | Storage | |
| `\x06DataSpaces/DataSpaceMap` | Stream | 80 |
| `\x06DataSpaces/DataSpaceInfo/\x09DRMDataSpace` | Stream | 40 |
| `\x06DataSpaces/TransformInfo/\x09DRMTransform/\x06Primary` | Stream | 46368 |

`Data`, `\x09DRMViewerContent`, `EncryptionInfo` and `EncryptedPackage` are
absent. Other root entries are the standard CompObj/summary streams and three
unrelated storages (names redacted). License stream names and contents are not
reported. The map references root DRMContent and DRMDataSpace; its definition
references DRMTransform. The transform header identifies the documented DRM
GUID, name, type and version.

Both Go and browser preflight returned `readable` before the patch, and `drm`
after it, using the real private sample. The existing guard only admitted the
OOXML DRM hierarchy when EncryptedPackage existed. It then accepted the
unencrypted compatibility WordDocument FIB as readable.

Production at observation: APP and Frontend revision
`76e9fca666095a43a642af90d06f24a2e746ba09`, APP healthy. APP image
`sha256:bf2ddb1a02a9cd9c2119d6b12af5835e8101b3b9505d2f31acb75682fbc1fcf3`;
Frontend image
`sha256:0a128fa68fad8286387870bd4125c00471b532435b1d1c24c790b76aaceb8d2f`.
These are the pre-repair images, not proof of a deployment.

Active document: completed/enabled, summary completed, one text chunk (111
characters), one summary chunk (192 characters), five enabled embedding/index
rows. Both chunks contain the permission notice; the database has a BM25 index
on embeddings. No Wiki page referenced the document ID. Actual search interface
acceptance must be recorded separately; database evidence is not an API test.

## Implementation

Go and TypeScript follow root-relative live paths and verify the classic map,
data space definition and DRM transform header before inspecting the FIB. The
content stream must be readable through the bounded CFB reader. Viewer content
is optional. Embedded objects, orphaned directory entries and stream names
alone do not prove protection. Existing limits and classification 400/2400
remain in use.

An independent parsed-text guard recognizes the complete Chinese compatibility
notice, allowing whitespace/punctuation wrapping and repeated copies, only
when no other text remains. Related words or a quotation accompanied by real
body text remain accepted. It runs before images/chunks/index/summary/wiki and
at the staging and temporary-document boundaries. Asynchronous reparse also
checks the stored binary before calling a parser. Typed refusals use the
existing permanent encrypted-file failure state.

Fixtures are entirely generated from structural constants in `generate.py`;
they contain no original production bytes or license payload. Existing vendor
fixtures retain their recorded license/provenance.

Protocol references: [MS-OFFCRYPTO map](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offcrypto/dd37820d-8056-4460-86c0-066953c64743),
[binary transform](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offcrypto/038fa5b0-a096-4f21-ba45-e48a03ec43cd).

## Historical repair plan

Retain the original and its hash and snapshot the exact document, chunks,
embedding metadata, audit records and Wiki references privately. Use the
existing authenticated single-document ReparseKnowledge lifecycle after the
patched runtime is verified. That flow removes the old retrieval data through
the retrieval/chunk services, resets the document to disabled, clears the
summary and queues processing without deleting the original. The patched worker
must record a permanent protected-file parsing failure before DocReader. Verify
the original hash/submitter/upload time, zero live chunks/index rows, no Wiki
references, and no recall using real Hybrid Search and Personal MCP. Never use
direct SQL deletion. If access, lifecycle safety or a required verification is
unavailable, record NOT_RUN and leave the original intact.

## Validation and release status

Initial Go detector suite PASS. Initial frontend suite: 57 PASS, 0 SKIP.
Real private DOC classification Go/TypeScript: before readable, after drm.
Further business regression, Chromium, native parser, CI, merge, image build,
deployment and historical repair are pending; none is claimed PASS here.
Private evidence directory: `E:/AIData/Codex/classic-irm-20261127`.


## Development verification update

Go detector regression and 10-second fuzz: PASS. Frontend final structural,
classification, timeout, batch and admission tests: 62 PASS, 0 SKIP. Chromium
harness: PASS after updating its pre-existing obsolete “encrypted file” label
assertion to the current “restricted file” label. Both classic variants and a
renamed classic file returned 2400 with zero extra HTTP requests. Initial local
Chromium navigation timeout and obsolete-label failure are retained in private
receipts; neither is counted as PASS.

Windows service regression: BUILD_FAILED because the installed MinGW linker
cannot find `-lrstrtmgr` required by DuckDB; service/HTTP/MCP verification must use
the Linux workflows and cannot be inferred from the detector pass.

Real production text chunk matches the full normalized notice with zero other
body characters. Before repair, both actual Hybrid Search and Go Personal MCP
keyword search recalled the selected knowledge and permission notice (two
queries each). No production data has been modified at this point.

Upstream/main was refreshed and checked for classic DRMContent/DRMDataSpace
implementation; no matching implementation was found. PR:
https://github.com/suoak/WeKnora/pull/283 . Exact-head Linux, native and general
checks are still running. Merge, deployment, post-deploy real sample acceptance
and historical repair remain pending.

Changed runtime paths: `internal/officeguard/{detect,classic,validate,placeholder}.go`,
`frontend/src/utils/officeEncryption.ts`,
`internal/application/service/{knowledge_process,knowledge_parse_failure,feishu_document_failure,temporary_document,office_sync}.go`.
Other changes are the generated synthetic fixtures, generator/manifest, focused
Go/TypeScript/Chromium tests and this incident report. No schema migration,
authorization model, source lease/cursor/lock, format allowlist or 8443 ingress
configuration changes are included.
