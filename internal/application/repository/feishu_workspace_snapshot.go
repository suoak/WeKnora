package repository

import (
	"context"
	"encoding/json"
	"os"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CheckWorkspaceImportSnapshot recognizes an already accepted export. The
// destination, durable dispatch, manifest identity and registered local file
// must agree. This does not grant content access: the consumer still verifies
// the manifest/file bytes, parser outputs and publication generation itself.
// Source OAuth is required to acquire the export, not to process its copy.
func CheckWorkspaceImportSnapshot(ctx context.Context, tx *gorm.DB,
	m *types.FeishuPersonalImport, a *types.FeishuPersonalAttempt, lock bool,
) (bool, error) {
	if os.Getenv("FEISHU_PERSONAL_WORKSPACE_IMPORT_SNAPSHOTS") == "0" || tx == nil || tx.Name() != "postgres" ||
		m == nil || a == nil || m.AuthMode != string(types.FeishuUserOAuth) ||
		a.AuthMode != types.FeishuUserOAuth || m.Status == "source_managed" ||
		m.ID != a.ImportID || m.ApplicationID != "" || a.ApplicationID != "" || a.GrantEpoch == "" ||
		a.CandidateVersionID == "" || a.CandidateVersionID == a.LogicalKnowledgeID ||
		m.KnowledgeID == "" || m.KnowledgeID != a.LogicalKnowledgeID ||
		m.SourceModified != a.Revision || a.ExpectedActiveVersion != "" || a.ManifestHash == "" || a.ManifestSize <= 0 {
		return false, nil
	}
	switch a.Status {
	case "index_pending", "processing", "needs_attention", "indexed":
	default:
		return false, nil
	}
	var row struct {
		FilePath, AttemptID, ManifestHash, ParseStatus string
		FileSize                                       int64
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE OF k,t,b"
	}
	err := tx.WithContext(ctx).Raw(`SELECT k.file_path,k.file_size,k.parse_status,
k.metadata->>'feishu_mvp_attempt' AS attempt_id,
k.metadata->>'feishu_manifest_sha256' AS manifest_hash
FROM knowledges k JOIN knowledge_bases b ON b.id=k.knowledge_base_id
JOIN tenants t ON t.id=k.tenant_id
WHERE k.id=? AND k.tenant_id=? AND b.id=? AND b.tenant_id=k.tenant_id
AND k.channel='feishu_personal' AND k.deleted_at IS NULL AND b.deleted_at IS NULL
AND t.status='active' AND t.deleted_at IS NULL`+suffix,
		m.KnowledgeID, a.CallerTenantID, m.KBID).Scan(&row).Error
	if err != nil {
		return false, err
	}
	handle, registered := types.ParseResourcePath(row.FilePath)
	if !registered {
		return false, nil // Export/registration has not completed yet.
	}
	if row.AttemptID != a.ID || row.ManifestHash != a.ManifestHash || row.FileSize != a.ManifestSize ||
		row.ParseStatus == types.ParseStatusDeleting || row.ParseStatus == types.ParseStatusCancelled {
		return false, ErrPersonalIdentity
	}
	query := func() *gorm.DB {
		db := tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx)
		if lock {
			db = db.Clauses(clause.Locking{Strength: "SHARE"})
		}
		return db
	}
	var file types.StoredResource
	if err := query().Where("handle=? AND tenant_id=? AND state='active' AND deleted_at IS NULL",
		handle, a.CallerTenantID).Take(&file).Error; err != nil {
		return false, err
	}
	if file.Provider != "local" || file.Size != a.ManifestSize ||
		file.Lifecycle != types.ResourceLifecyclePersistent || file.ExpiresAt != nil {
		return false, ErrPersonalIdentity
	}
	var bindings []types.ResourceBinding
	if err := query().Where("resource_id=?", file.ID).Limit(2).Find(&bindings).Error; err != nil {
		return false, err
	}
	if len(bindings) != 1 || bindings[0].TenantID != a.CallerTenantID ||
		bindings[0].OwnerType != types.ResourceOwnerKnowledge || bindings[0].OwnerID != m.KnowledgeID ||
		bindings[0].Relation != types.ResourceRelationSourceFile {
		return false, ErrPersonalIdentity
	}
	var op types.TaskPendingOp
	if err := query().Where("scope=? AND scope_id=? AND tenant_id=?", types.KnowledgeDispatchScope,
		m.KnowledgeID, a.CallerTenantID).Take(&op).Error; err != nil {
		return false, err
	}
	var dispatch types.KnowledgeDispatch
	if json.Unmarshal(op.Payload, &dispatch) != nil || dispatch.PayloadVersion != 3 ||
		dispatch.AttemptID != a.ID || dispatch.Revision != a.Revision ||
		dispatch.Task.PersonalAttemptID != a.ID || !dispatch.Task.FirstCreate ||
		dispatch.Task.KnowledgeID != m.KnowledgeID || dispatch.Task.KnowledgeBaseID != m.KBID ||
		dispatch.Task.TenantID != a.CallerTenantID || dispatch.Task.CandidateVersionID != a.CandidateVersionID {
		return false, ErrPersonalIdentity
	}
	return true, nil
}
