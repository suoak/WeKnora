-- Data-preserving rollback: deploy the previous application while retaining
-- these additive columns and resource dimensions. Older code ignores them.
-- Deliberately do not remove events, links, columns or collection epochs.
SELECT 1;
