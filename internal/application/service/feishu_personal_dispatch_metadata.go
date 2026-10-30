package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Dispatch metadata is protocol state, not a readable document. In particular,
// never select content, summary, profile, source, title or arbitrary metadata.
// This DTO cannot be passed to a parser, a model or a content response.
type personalDispatchMetadata struct {
	ID, KnowledgeBaseID, FilePath, FileName, FileHash, ParseStatus string
	AttemptID                                                      string
	ManifestSHA256                                                 string `gorm:"column:manifest_sha256"`
	TenantID                                                       uint64
	FileSize                                                       int64
}

func (p *PersonalRecovery) loadDispatchMetadata(
	ctx context.Context, tenant uint64, kb, knowledge string,
) (*personalDispatchMetadata, error) {
	return loadPersonalDispatchMetadata(p.service.db.WithContext(ctx), tenant, kb, knowledge, false)
}

func loadPersonalDispatchMetadata(
	db *gorm.DB,
	tenant uint64,
	kb, knowledge string,
	locked bool,
) (*personalDispatchMetadata, error) {
	if tenant == 0 || kb == "" || knowledge == "" {
		return nil, repository.ErrPersonalIdentity
	}
	var row personalDispatchMetadata
	query := db.Table("knowledges")
	if locked {
		query = query.Clauses(clause.Locking{Strength: "SHARE"})
	}
	result := query.
		Select(`id, tenant_id, knowledge_base_id, file_path, file_name, file_hash, file_size,
parse_status, metadata->>'feishu_mvp_attempt' AS attempt_id,
metadata->>'feishu_manifest_sha256' AS manifest_sha256`).
		Where("id=? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL", knowledge, tenant, kb).
		Take(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if row.ID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	return &row, nil
}

// Authorization needs persisted ownership, not the KB's generated profile or
// description. Canonical ResolveKB and RequireKBWrite remain authoritative.
func (p *PersonalRecovery) loadAuthorizationKB(
	ctx context.Context,
	id string,
) (*types.KnowledgeBase, error) {
	return personalAuthorizationKB(p.service.db.WithContext(ctx), id, false)
}

func personalAuthorizationKB(db *gorm.DB, id string, locked bool) (*types.KnowledgeBase, error) {
	var row struct {
		ID, CreatorID string
		TenantID      uint64
	}
	query := db.Table("knowledge_bases")
	if locked {
		query = query.Clauses(clause.Locking{Strength: "SHARE"})
	}
	if err := query.
		Select("id, tenant_id, creator_id").
		Where("id=? AND deleted_at IS NULL", id).Take(&row).Error; err != nil {
		return nil, err
	}
	owner, err := types.PrivateKBOwner(db, row.ID)
	if err != nil {
		return nil, err
	}
	return &types.KnowledgeBase{
		ID:             row.ID,
		TenantID:       row.TenantID,
		CreatorID:      row.CreatorID,
		PrivateOwnerID: owner,
	}, nil
}
