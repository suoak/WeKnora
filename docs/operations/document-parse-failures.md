# Document parse failures and recovery progress

Ordinary document parsing records a safe diagnosis in
`metadata.parse_failure_v1`: `code`, `state`, `action`, `attempts`,
`max_attempts`, and `auto_retry`. This metadata is server-owned and contains
no original file paths, source URLs, credentials, or raw parser output.

Temporary transport errors, rate limits, and unclassified parser failures use
the existing finite task retry budget. An error reported inside `ReadResult`
uses that same retry path rather than silently acknowledging the failed task.
The card shows automatic retry progress only for a processing row with an
explicit retry record and budget remaining. Never infer scheduled recovery
from an error string or a historical failed row.

Known encryption, format mismatch, semantic row limits, blocked addresses,
and parser resource exhaustion terminate identical automatic attempts early.
They explain the problem and the required action. Unknown failures do not claim
that a file is corrupt. Once retry attempts are exhausted, the processing trace
remains available for investigation and the card requires attention.

The durable dispatch must enter `attention` for terminal failures as well. An
archived queue task must not become a new recovery attempt with a reset budget
after restart. SQLite and real PostgreSQL tests cover both terminal failures
and still-retryable siblings; unrelated pending work continues recovering.

A cancelled, deleting, or replaced source file must not be revived by the
failure recorder. Starting a new processing run removes stale recovery
metadata. Existing process overrides and source metadata remain intact.

This workflow does not authorize ordinary destructive reparsing of controlled
Feishu personal/source documents. Those protocols retain their own source
validation, bounded recovery, staging, and publication mechanisms. A complete
per-document Feishu reparse feature must be verified separately before release.

The 2026-11-15 recovery audit also identified a parser compatibility case:
blank underline declarations in shared-string rich text make openpyxl refuse
three otherwise readable XLSX documents. The compatibility normalization
preserves all shared-string text, CR/LF, worksheets and original uploaded
bytes; both builtin and MarkItDown readers use it. It does not bypass encrypted
files or parser resource limits.
