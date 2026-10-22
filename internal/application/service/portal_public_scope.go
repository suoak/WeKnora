package service

import (
	"context"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

type PortalPublicScope struct {
	Rows                       []PortalPublicKB `json:"knowledge_bases"`
	PortalPublished            bool             `json:"portal_published"`
	ExceptionManagementEnabled bool             `json:"exception_management_enabled"`
	Rule                       string           `json:"rule"`
}
type PortalPublicKB struct {
	KBID           string    `json:"knowledge_base_id"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	Documents      int64     `json:"documents"`
	ReadyDocuments int64     `json:"ready_documents"`
	Blocked        bool      `json:"public_blocked"`
	Revision       int64     `json:"revision"`
	Reason         string    `json:"reason"`
	UpdatedBy      string    `json:"updated_by"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (s *portalService) PublicKnowledgeScope(ctx context.Context, tenant uint64) (*PortalPublicScope, error) {
	if _, err := s.PublicationPermissions(ctx, tenant); err != nil {
		return nil, err
	}
	out := &PortalPublicScope{Rows: []PortalPublicKB{}, Rule: "portal_published_default", ExceptionManagementEnabled: s.publicationExceptionConfig.Enabled && s.publicationExceptionConfig.ContractRef != ""}
	var published int64
	if err := s.db.WithContext(ctx).Table("tenant_portal_configs pc").Joins("JOIN tenants t ON t.id=pc.tenant_id AND t.deleted_at IS NULL AND t.status='active'").Where("pc.tenant_id=? AND pc.status='published'", tenant).Count(&published).Error; err != nil {
		return nil, err
	}
	out.PortalPublished = published == 1
	err := s.db.WithContext(ctx).Table("knowledge_bases kb").Select(`kb.id AS kb_id,kb.name,kb.type,
 (SELECT count(*) FROM knowledges k WHERE k.knowledge_base_id=kb.id AND k.tenant_id=kb.tenant_id AND k.deleted_at IS NULL) AS documents,
 (SELECT count(*) FROM knowledges k WHERE k.knowledge_base_id=kb.id AND k.tenant_id=kb.tenant_id AND k.deleted_at IS NULL AND k.parse_status='completed' AND k.enable_status='enabled') AS ready_documents`).Where("kb.tenant_id=? AND kb.deleted_at IS NULL AND kb.is_temporary=false", tenant).Order("kb.id").Find(&out.Rows).Error
	if err != nil {
		return nil, err
	}
	if !s.db.Migrator().HasTable("kb_publication_policies") {
		out.ExceptionManagementEnabled = false
		return out, nil
	}
	for i := range out.Rows {
		row := &out.Rows[i]
		var p types.EnterprisePolicy
		if err := s.db.WithContext(ctx).Table("kb_publication_policies").Where("knowledge_base_id=?", row.KBID).Find(&p).Error; err != nil {
			return nil, err
		}
		row.Blocked = types.ExplicitPortalPublicBlock(p)
		row.Revision = p.Revision
		row.Reason = p.Reason
		row.UpdatedBy = p.UpdatedBy
		row.UpdatedAt = p.UpdatedAt
	}
	return out, nil
}

func (s *portalService) SetPublicKnowledgeException(ctx context.Context, tenant uint64, kb string, blocked bool, revision int64, reason string) error {
	actor, err := portalJWTActor(ctx)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		authority := &PortalPolicyAuthority{DB: tx, Config: s.publicationExceptionConfig}
		if err := authority.AuthorizeEnterprisePolicyChange(ctx, actor, types.EnterprisePolicyTarget{Kind: "space", ID: strconv.FormatUint(tenant, 10)}, "revoke"); err != nil {
			return err
		}
		return repository.NewEnterpriseAccessRepository(tx).SetPortalKBPublicBlock(ctx, tenant, kb, actor, reason, revision, blocked)
	})
}
