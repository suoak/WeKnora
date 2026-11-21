# Portal asset counts

`file_count` retains its original discovery contract: live file/file_url rows in
the pending, processing, finalizing or completed states, below non-temporary,
non-deleted knowledge bases. Existing clients must not interpret it as all
document entries.

The additive fields are:

| Field | Meaning |
|---|---|
| document_entry_count | Live file, file_url, manual and url entries in document knowledge bases, excluding deleting. Failed and cancelled entries remain included while retained in the list. |
| uploaded_file_count | The file/file_url subset of document_entry_count. |
| faq_knowledge_base_count | Effective FAQ knowledge bases, separate from document entries. |
| wiki_knowledge_base_count | Effective Wiki-type knowledge bases, separate from document entries and generated Wiki pages. |
| asset_count_scope | space_total for authorized workspace management totals; hidden when these new totals are not disclosed. |

New workspace totals require system-administrator or active workspace
owner/administrator authority. Discovery, ordinary membership, restricted API
Key scopes and document-scoped contexts do not grant access to these totals.
Subjects participating in document grants receive no unfiltered workspace total.
Missing numeric fields mean undisclosed, not zero. The UI renders an em dash and
does not fall back to legacy file_count. These totals are distinct from the
authorized and filtered `total` of a knowledge-list request. No document read
permission is granted by any count field.

Lists, cards and Portal aggregation share the live-entry deletion predicate.
Folders and filters remain explicit scopes: the detail header identifies the
current folder or filtered result and the full knowledge-base count returned by
the existing authorized folder API. It does not derive the full count from one
page of results or from an administrative total.

Portal entry, browser activation and a visible-page 60-second interval refresh
the overview. Successful asset mutations invalidate its generation; user,
credential and tenant changes synchronously clear its data. Concurrent refreshes
are shared, and obsolete responses cannot publish data into a newer session or
overwrite newer list/overview requests. No server-side cache or database
migration is introduced.

Regression coverage includes repository type/status/deletion matrices, real
JWT/Auth/RBAC/document-grant HTTP fixtures on a formally migrated temporary DB,
and actual Vue/Pinia/browser lifecycle tests. Browser response fixtures test
transport and rendering behavior; they do not substitute for authorization
acceptance. Production writes and synthetic production documents are not part
of the release acceptance procedure.
