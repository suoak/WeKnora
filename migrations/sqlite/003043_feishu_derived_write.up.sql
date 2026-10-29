-- Controlled post-publication writeback requires PostgreSQL transaction IDs,
-- row locks and the atomic publication fence. SQLite retains its existing
-- deny-all triggers for enrolled Feishu resources; ordinary summaries are
-- unaffected. Do not emulate this authority with a connection-local flag.
SELECT 1;
