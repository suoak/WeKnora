# Bounded Feishu export downloads

The customer-research document export is approximately 47 MB. A bounded diagnostic
using the existing authorized test application reproduced HTTP 200 body timeouts
from the production network: each 30-second attempt read only 35–36 MB. The same
client downloaded the complete export from the packaging server in about 19
seconds. Authentication, export creation, polling and initial response succeeded;
the failed phase was downloading the response body.

A controlled production read probe with `Range: bytes=0-1048575` returned HTTP 206,
`Content-Range: bytes 0-1048575/47099020`, and exactly 1048576 bytes. The endpoint
advertised byte ranges but supplied neither ETag nor Last-Modified. This evidence
supports bounded range requests against one export-file token. The probe did not
trigger knowledge synchronization, model calls or production database changes.

Only the export-file download path now requests consecutive parts of at most
8 MiB. Every request uses the existing request executor, permission/identity
revalidation, SSRF transport, app pacing, cancellation and finite retry policy.
The existing 30-second HTTP attempt deadline is unchanged. The existing 512 MiB
whole-file cap bounds the number of parts at 64. Normal online model paths, media
downloads and uploaded Drive file downloads are unchanged.

Each successful partial response must have the exact requested start and end,
consistent total length, matching payload length and identity content encoding.
ETag and Last-Modified, when returned, must remain consistent; strong ETags also
produce If-Match requests. All parts address the same export-file token created
by one export task. A first response of HTTP 200 retains the prior whole-response
behavior for servers that ignore Range. A later HTTP 200, inconsistent range,
changed validator, oversized response, cancellation or exhausted retry returns
an error and no partial file. Parsing and version admission receive bytes only
after the complete export is assembled. Version ownership and atomic publication
are unchanged.

Tests cover ordered assembly, the whole-response compatibility path, 429 retry
and finite exhaustion, wrong offsets, changed totals and validators, missing
Content-Range, short/oversized parts, unexpected HTTP 200 after a partial
response, encoded ranges, the file-size cap and cancellation. Real multipart
download and DOCX ZIP integrity must also be verified before production rollout.

References: [Feishu export download endpoint](https://open.feishu.cn/document/server-docs/docs/drive-v1/export_task/download).
Production response behavior above is measured evidence, rather than an inferred
guarantee about every Feishu region or endpoint.
