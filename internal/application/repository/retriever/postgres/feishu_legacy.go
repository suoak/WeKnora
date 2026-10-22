package postgres

// Applied inside the actual BM25/vector candidate query. A later output filter
// cannot protect model input, scores, titles or snippets from a held KB.
const legacySourceCandidatePredicate = `NOT EXISTS (
 SELECT 1 FROM feishu_legacy_resources legacy_r WHERE legacy_r.knowledge_base_id=embeddings.knowledge_base_id
 AND legacy_r.state IN ('hold','revoked')) AND NOT EXISTS (
 SELECT 1 FROM data_sources legacy_d WHERE legacy_d.knowledge_base_id=embeddings.knowledge_base_id
 AND legacy_d.legacy_access_state IN ('hold','revoked'))`
