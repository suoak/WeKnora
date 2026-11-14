# ELOG IM catalog count after retrieval recovery

The repaired IM search returns ELOG chunks, but its prompt still says zero
documents: generic catalog listing independently sees a retained receipt for
a deleted personal import, selects the personal metadata path, and refuses the
IM principal. Prompt construction silently defaults that failed listing to 0.

Use the existing live receipt selector for catalog routing as well. Missing,
deleted and deleting records cannot select personal recovery. Live personal
publications retain their existing reader/metadata checks. Represent failed
catalog lookups as unknown in the prompt rather than an invented empty count;
successful empty and populated counts retain their numerical values.

Validate deleted-receipt IM page routing, failed-catalog prompt input, unknown
versus zero prompt rendering, successful/shared catalog counts and exact search
routing under race. No source, document, membership or publication permission
is broadened. Build from merged main and verify schema-3047 startup before
replacing the APP. DocReader changes are unnecessary for this fix.
