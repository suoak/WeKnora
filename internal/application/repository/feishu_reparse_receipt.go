package repository

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// ConfirmSourceReparsePublication acknowledges only an already committed
// publication. It neither writes a cursor nor changes a retry allocation.
func (s *FeishuLegacyStore) ConfirmSourceReparsePublication(
	ctx context.Context, ds *types.DataSource, root, externalID, version string,
) (bool, error) {
	seconds, err := strconv.ParseInt(version, 10, 64)
	if err != nil || seconds <= 0 || strconv.FormatInt(seconds, 10) != version || ds == nil {
		return false, nil
	}
	config, err := ds.ParseConfig()
	if err != nil {
		return false, err
	}
	if config == nil || root == "" || externalID == "" || !slices.Contains(config.ResourceIDs, root) {
		return false, nil
	}
	confirmed := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.execution(ctx, tx, ds); err != nil {
			return err
		}
		var rows []struct {
			Payload        json.RawMessage
			Revision       string
			SourceRevision string
			KnowledgeID    string
			AttemptID      string
		}
		query := tx.Table("task_pending_ops o").
			Select("o.payload,a.revision,r.source_revision,r.knowledge_id,a.id AS attempt_id").
			Joins("JOIN feishu_legacy_resources r ON r.data_source_id=o.scope_id AND r.knowledge_id=o.dedup_key").
			Joins("JOIN feishu_legacy_attempts a ON a.id=r.active_attempt AND a.resource_id=r.id").
			Joins("JOIN knowledges k ON k.id=r.knowledge_id AND k.tenant_id=r.tenant_id"+
				" AND k.knowledge_base_id=r.knowledge_base_id").
			Where("o.task_type=? AND o.scope=? AND o.scope_id=? AND o.tenant_id=? AND o.op='completed'",
				sourceDocumentReparseTask, sourceDocumentReparseScope, ds.ID, ds.TenantID).
			Where("r.tenant_id=? AND r.knowledge_base_id=? AND r.external_id=? AND r.state='active'",
				ds.TenantID, ds.KnowledgeBaseID, externalID).
			Where("o.payload->>'published_attempt'=a.id").
			Where("r.current_attempt=r.active_attempt AND a.status='published'").
			Where("k.deleted_at IS NULL AND k.parse_status='completed' AND k.enable_status='enabled'").
			Order("o.id DESC").Limit(2)
		if err := query.Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return nil
		}
		row := rows[0]
		var request types.FeishuDocumentReparse
		if err := json.Unmarshal(row.Payload, &request); err != nil {
			return legacyFence("document_request_invalid")
		}
		if request.DataSourceID != ds.ID || request.TenantID != ds.TenantID ||
			request.KnowledgeBaseID != ds.KnowledgeBaseID || request.KnowledgeID != row.KnowledgeID ||
			request.ExternalID != externalID || !slices.Contains(config.ResourceIDs, request.RootID) ||
			request.Published != row.AttemptID ||
			request.ConfigHash != LegacySourceReparseConfigHash(ds) {
			return nil
		}
		wanted := time.Unix(seconds, 0).UTC()
		for _, revision := range []string{row.Revision, row.SourceRevision} {
			published, err := time.Parse(time.RFC3339Nano, revision)
			if err != nil || !published.Equal(wanted) {
				return nil
			}
		}
		var kb types.KnowledgeBase
		if err := tx.Where("id=? AND tenant_id=? AND deleted_at IS NULL", ds.KnowledgeBaseID,
			ds.TenantID).First(&kb).Error; err != nil {
			return err
		}
		confirmed = request.KBConfigHash == LegacyKBConfigHash(&kb)
		return nil
	})
	return confirmed, err
}
