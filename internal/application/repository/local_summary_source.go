package repository

import (
	"context"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/types"
)

// LocalSummarySourceIdentity is a content-free authority snapshot for an
// ordinary workspace document. It cannot authorize connector or personal
// imports, override a private-KB check, or grant a whole-tenant model read.
func (r *knowledgeRepository) LocalSummarySourceIdentity(
	ctx context.Context, tenant uint64, kb, knowledge, expectedMetadata string,
) (string, bool, error) {
	if !types.LegacyReadGuardsEnabled(r.db) {
		return "", false, nil
	}
	if _, internal := types.SyncExecutionFromContext(ctx); internal {
		return "", false, nil
	}
	current, valid := types.TenantIDFromContext(ctx)
	if !valid || current != tenant || tenant == 0 || kb == "" || knowledge == "" {
		return "", true, types.ErrLegacySourceRestricted
	}
	db := r.db.WithContext(ctx)
	var row struct {
		ID, KnowledgeBaseID, Type, Channel, Title, FileName, FileHash, FilePath, Source string
		TenantID                                                                        uint64
		Metadata, CustomMetadata                                                        types.JSON
		ParseStatus, EnableStatus                                                       string
	}
	if err := db.Table("knowledges").Select(
		"id,tenant_id,knowledge_base_id,type,channel,title,file_name,file_hash,file_path,source,"+
			"metadata,custom_metadata,parse_status,enable_status",
	).Where("id=? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL", knowledge, tenant, kb).
		Take(&row).Error; err != nil {
		return "", true, err
	}
	switch row.Channel {
	case "", types.ChannelWeb, types.ChannelAPI, types.ChannelBrowserExtension:
	default:
		return "", false, nil
	}
	var metadata map[string]any
	if len(row.Metadata) != 0 && json.Unmarshal(row.Metadata, &metadata) != nil {
		return "", false, nil
	}
	for _, key := range []string{"datasource_id", "external_id", "personal_attempt_id", "personal_import_id"} {
		if value, exists := metadata[key]; exists && value != nil && value != "" {
			return "", false, nil
		}
	}
	if row.EnableStatus != "enabled" || (row.ParseStatus != "processing" &&
		row.ParseStatus != "finalizing" && row.ParseStatus != "completed") {
		return "", true, types.ErrLegacySourceRestricted
	}
	var live bool
	if err := db.Raw(`SELECT EXISTS(SELECT 1 FROM knowledge_bases k JOIN tenants t ON t.id=k.tenant_id
 WHERE k.id=? AND k.tenant_id=? AND k.deleted_at IS NULL AND t.deleted_at IS NULL AND t.status='active')`,
		kb, tenant).Scan(&live).Error; err != nil {
		return "", true, err
	}
	if !live {
		return "", true, types.ErrLegacySourceRestricted
	}
	if err := types.CheckLegacySourceRead(db, knowledge, kb); err != nil {
		return "", true, err
	}
	// A channel value alone is never source evidence. Refuse the narrower
	// capability if any current or historical external-source binding exists.
	var external bool
	if err := db.Raw(`SELECT
 EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE tenant_id=? AND
 (knowledge_id=? OR (?<>'' AND original_file_path=?))) OR
 EXISTS(SELECT 1 FROM feishu_personal_imports WHERE knowledge_id=?) OR
 EXISTS(SELECT 1 FROM feishu_personal_resources WHERE logical_knowledge_id=?) OR
 EXISTS(SELECT 1 FROM task_pending_ops WHERE scope=? AND scope_id=?)`,
		tenant, knowledge, row.FilePath, row.FilePath, knowledge, knowledge,
		types.PersonalExactReceiptScope, knowledge).Scan(&external).Error; err != nil {
		return "", true, err
	}
	if external {
		return "", true, types.ErrLegacySourceRestricted
	}
	currentMetadata := (&types.Knowledge{CustomMetadata: row.CustomMetadata}).CustomMetadataText()
	if currentMetadata != expectedMetadata {
		return "", true, types.ErrLegacySourceRestricted
	}
	version, owner, err := resourceVersion(db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: knowledge})
	if err != nil {
		return "", true, err
	}
	if owner != tenant {
		return "", true, types.ErrLegacySourceRestricted
	}
	// Lifecycle progress can legitimately advance while a summary is running.
	// The trigger-backed version still invalidates any in-flight stale proof.
	row.ParseStatus, row.EnableStatus = "", ""
	return wikiDigest([]any{row, version}), true, nil
}
