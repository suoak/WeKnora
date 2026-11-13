# ELOG IM search routing after a deleted personal import

The ELOG Feishu IM channel uses its own workspace's Smart Reasoning agent.
Its search tools returned published_viewer_membership_lookup/forbidden, then
the model incorrectly reported an empty library. The library has published
connector documents. A retained personal exact receipt points to a document
deleted on November 10; its existence incorrectly selects the personal
publication recovery fan-out for ordinary single- and multi-KB searches.

Select that fan-out only from receipts joined to a live document with matching
tenant and KB bindings. Ignore missing, soft-deleted and deleting documents
when selecting the search path and enumerating recovered document IDs. Retain
all existing ordinary source/KB/document guards and personal publication viewer
checks. This changes routing, grants no identity, and retains recovery records.

Regression validation reproduces the old failure using an IM principal and a
deleted import in an isolated SQLite fixture. Fixed single-/multi-KB routing
must fall through to ordinary search; a live receipt must still select the
protected exact path. Missing targets, wrong tenant/KB bindings, selected-file
scope and pending deletion are tested independently under the race detector.
Existing published-viewer permission tests remain part of validation.

After merge, validate the combined current main with the WordML/extraction
repair and build/start it against schema 3047 before production deployment.
User-driven IM retesting is required to confirm the actual channel response;
an empty result or failed tool call must never be described as an empty KB.
