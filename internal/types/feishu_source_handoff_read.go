package types

import (
	"encoding/json"

	"gorm.io/gorm"
)

// VerifiedSourceHandoff identifies the published first import handed to the
// ordinary source pipeline. It is not a grant: private KB, document ACL and
// legacy source/revocation checks must still run. Exact personal publications
// and API/MCP callers retain their existing serving-proof path.
func VerifiedSourceHandoff(db *gorm.DB, knowledge, kb string) (bool, error) {
	tenant := WorkspaceWebReadTenant(db.Statement.Context)
	effective, ok := TenantIDFromContext(db.Statement.Context)
	if tenant == 0 || !ok || effective != tenant {
		return false, nil
	}
	db = db.Session(&gorm.Session{NewDB: true, Context: db.Statement.Context})
	var rows []struct {
		Metadata                                    JSON
		FileHash, AttemptID, ManifestHash, SourceID string
		Payload                                     json.RawMessage
	}
	caller := CallerFromContext(db.Statement.Context)
	err := db.Raw(`SELECT k.metadata,k.file_hash,a.id AS attempt_id,a.manifest_hash,o.payload,i.id AS source_id
FROM knowledges k
JOIN feishu_personal_imports i ON i.knowledge_id=k.id AND i.kb_id=k.knowledge_base_id AND i.status='source_managed'
JOIN data_sources d ON d.id=i.id AND d.tenant_id=k.tenant_id
 AND d.knowledge_base_id=k.knowledge_base_id AND d.deleted_at IS NULL
JOIN feishu_personal_resources r ON r.import_id=i.id AND r.logical_knowledge_id=k.id
JOIN feishu_personal_attempts a ON a.id=r.current_attempt_id AND a.import_id=i.id
 AND a.logical_knowledge_id=k.id AND a.caller_tenant_id=k.tenant_id
JOIN task_pending_ops o ON o.tenant_id=k.tenant_id AND o.scope='personal_index_receipt'
 AND o.scope_id=k.id AND o.dedup_key='index-' || a.id AND o.op='verified'
WHERE k.id=? AND k.knowledge_base_id=? AND k.tenant_id=? AND k.deleted_at IS NULL
 AND k.channel='feishu_personal' AND k.parse_status=? AND k.enable_status='enabled'
 AND d.type IN ('feishu','feishu_drive','lark','lark_drive')
 AND d.legacy_access_state IN ('unmanaged','active','hold')
 AND a.status='obsolete' AND a.owner_nonce='' AND a.lease_until IS NULL AND r.active_version_id=''
 AND r.candidate_version_id=a.candidate_version_id AND a.candidate_version_id<>''
 AND NOT EXISTS(SELECT 1 FROM feishu_legacy_resources lr WHERE lr.knowledge_id=k.id)
 AND NOT EXISTS(SELECT 1 FROM task_pending_ops exact_receipt
 WHERE exact_receipt.scope=? AND exact_receipt.scope_id=k.id)
 AND EXISTS(SELECT 1 FROM tenant_members m JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id
 WHERE m.user_id=? AND m.tenant_id=k.tenant_id AND m.role=? AND m.status='active' AND m.deleted_at IS NULL
 AND u.is_active AND u.deleted_at IS NULL AND t.status='active' AND t.deleted_at IS NULL)
LIMIT 2`,
		knowledge, kb, tenant, ParseStatusCompleted, PersonalExactReceiptScope, caller.UserID, caller.Role,
	).Scan(&rows).Error

	if err != nil {
		return false, err
	}
	if len(rows) != 1 {
		return false, nil
	}
	// Match the persisted handoff binding without relying on JSON SQL dialects.
	var metadata map[string]any
	var receipt struct {
		Version     int    `json:"payload_version"`
		KnowledgeID string `json:"knowledge_id"`
		AttemptID   string `json:"attempt_id"`
		Manifest    string `json:"manifest_sha256"`
		FileHash    string `json:"file_hash"`
		Chunks      int    `json:"text_chunks"`
		Vectors     bool   `json:"pg_vector_rows_verified"`
		BM25        bool   `json:"bm25_document_readback"`
	}
	row := rows[0]
	if json.Unmarshal(row.Metadata, &metadata) != nil || json.Unmarshal(row.Payload, &receipt) != nil {
		return false, nil
	}
	return row.SourceID != "" && metadata["datasource_id"] == row.SourceID &&
		metadata["feishu_mvp_attempt"] == row.AttemptID && metadata["feishu_manifest_sha256"] == row.ManifestHash &&
		receipt.Version == 1 && receipt.KnowledgeID == knowledge && receipt.AttemptID == row.AttemptID &&
		receipt.Manifest != "" && receipt.Manifest == row.ManifestHash &&
		receipt.FileHash != "" && receipt.FileHash == row.FileHash &&
		receipt.Chunks > 0 && receipt.Vectors && receipt.BM25, nil
}
