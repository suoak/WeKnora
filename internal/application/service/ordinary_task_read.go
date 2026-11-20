package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

type ordinaryTaskSource struct {
	ID, KnowledgeBaseID, Channel, Type, Source, FilePath string
	TenantID                                             uint64
	LatestAttempt                                        int
	Metadata                                             types.JSON
}

func ordinaryTaskSourceEligible(row ordinaryTaskSource) bool {
	if row.Channel != types.ChannelWeb || row.Type != "file" || row.Source != "" || row.FilePath == "" {
		return false
	}
	metadata, err := row.Metadata.Map()
	if err != nil {
		return false
	}
	for key := range metadata {
		if key == "datasource_id" || key == "data_source_id" || key == "external_id" ||
			key == "source_resource_id" || strings.HasPrefix(key, "feishu") || strings.HasPrefix(key, "lark") {
			return false
		}
	}
	return true
}

// This runs only inside the existing trusted task middleware, after canonical
// tenant/parent resolution. Ordinary uploads do not inherit a sibling's Feishu
// hold, while connector, private and exact-publication protocols stay separate.
func (s *FeishuPersonalService) ordinaryTaskReadContext(
	ctx context.Context, binding privateTaskBinding,
) (context.Context, error) {
	if binding.KnowledgeID == "" || !types.LegacyReadGuardsEnabled(s.db) {
		return ctx, nil
	}
	load := func(db *gorm.DB) (ordinaryTaskSource, error) {
		var row ordinaryTaskSource
		err := db.Table("knowledges").Select(
			`id,tenant_id,knowledge_base_id,channel,type,source,file_path,metadata,
COALESCE((SELECT MAX(attempt) FROM knowledge_processing_spans s WHERE s.knowledge_id=knowledges.id),0) AS latest_attempt`,
		).Where("id=? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL",
			binding.KnowledgeID, binding.TenantID, binding.KnowledgeBaseID).Scan(&row).Error
		return row, err
	}
	initial, err := load(s.db.WithContext(ctx))
	if err != nil || !ordinaryTaskSourceEligible(initial) {
		return ctx, err
	}
	// FileHash also incorporates derived image annotations and may legitimately
	// change while sibling image tasks run. The immutable original locator and
	// current parse epoch distinguish replacement/reparse from that progress.
	// A first-create task can open attempt one after its initial metadata read.
	expectedAttempt := max(initial.LatestAttempt, 1)
	if binding.Attempt > 0 && binding.Attempt != expectedAttempt {
		return ctx, errors.Join(asynq.SkipRetry, types.ErrLegacySourceRestricted)
	}
	check := func(db *gorm.DB) error {
		row, err := load(db)
		if err != nil {
			return err
		}
		if !ordinaryTaskSourceEligible(row) || row.FilePath != initial.FilePath ||
			row.LatestAttempt < initial.LatestAttempt || row.LatestAttempt > expectedAttempt {
			return types.ErrLegacySourceRestricted
		}
		var allowed bool
		err = db.Raw(`SELECT EXISTS(SELECT 1 FROM knowledge_bases k JOIN tenants t ON t.id=k.tenant_id
WHERE k.id=? AND k.tenant_id=? AND k.deleted_at IS NULL AND t.status='active' AND t.deleted_at IS NULL)
AND NOT EXISTS(SELECT 1 FROM feishu_private_kbs WHERE kb_id=?)
AND NOT EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_id=?)
AND NOT EXISTS(SELECT 1 FROM feishu_personal_resources WHERE logical_knowledge_id=?)
AND NOT EXISTS(SELECT 1 FROM task_pending_ops WHERE scope=? AND scope_id=?)`,
			binding.KnowledgeBaseID, binding.TenantID, binding.KnowledgeBaseID,
			binding.KnowledgeID, binding.KnowledgeID, types.PersonalExactReceiptScope, binding.KnowledgeID).
			Scan(&allowed).Error
		if err != nil {
			return err
		}
		if !allowed {
			return types.ErrLegacySourceRestricted
		}
		return nil
	}
	if err := check(s.db.WithContext(ctx)); err != nil {
		return ctx, err
	}
	return types.WithOrdinaryTaskDocumentRead(ctx, binding.TenantID, binding.KnowledgeID,
		binding.KnowledgeBaseID, check)
}
