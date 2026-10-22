package service

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

type PortalPublicationPermissions struct {
	Owner            bool `json:"owner"`
	Reviewer         bool `json:"reviewer"`
	MutationsEnabled bool `json:"mutations_enabled"`
}

func (s *portalService) PublicationPermissions(ctx context.Context, tenant uint64) (PortalPublicationPermissions, error) {
	uid, err := portalJWTActor(ctx)
	if err != nil {
		return PortalPublicationPermissions{}, err
	}
	tx := s.db.WithContext(ctx)
	owner := portalLiveOwner(tx, uid, tenant)
	// Admin may inspect the existing management inventory even when policy
	// authority is disabled; only explicit configuration enables approvals.
	var count int64
	err = tx.Table("users").Where("id=? AND is_active=true AND is_system_admin=true AND deleted_at IS NULL", uid).Count(&count).Error
	admin := err == nil && count == 1 && types.IsSystemAdminFromContext(ctx)
	if !owner && !admin {
		return PortalPublicationPermissions{}, access.ErrForbidden
	}
	return PortalPublicationPermissions{Owner: owner, Reviewer: portalLiveReviewer(ctx, tx, uid, s.publicationConfig), MutationsEnabled: s.publicationConfig.Enabled && s.publicationConfig.ContractRef != ""}, nil
}

type PortalPublicationReviewSnapshot struct {
	TenantID            uint64                       `json:"tenant_id"`
	KBID                string                       `json:"knowledge_base_id"`
	Resources           []PortalReviewResource       `json:"resources"`
	State               string                       `json:"state"`
	ProposedBy          string                       `json:"proposed_by"`
	ReviewedBy          *string                      `json:"reviewed_by"`
	ReviewedAt          *time.Time                   `json:"reviewed_at"`
	Reason              string                       `json:"reason"`
	InheritNewOrdinary  bool                         `json:"inherit_new_ordinary"`
	ProposerActiveOwner bool                         `json:"proposer_active_owner"`
	Permissions         PortalPublicationPermissions `json:"permissions"`
}

func (s *portalService) PublicationReviewSnapshot(ctx context.Context, tenant uint64, kb string) (*PortalPublicationReviewSnapshot, error) {
	permissions, err := s.PublicationPermissions(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := &PortalPublicationReviewSnapshot{TenantID: tenant, KBID: kb, Permissions: permissions, State: types.EnterprisePolicyPending}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var exists int64
		if err := tx.Table("knowledge_bases").Where("id=? AND tenant_id=? AND deleted_at IS NULL AND is_temporary=false AND type='document'", kb, tenant).Count(&exists).Error; err != nil || exists != 1 {
			return access.ErrForbidden
		}
		var ids []string
		if err := tx.Table("knowledges").Where("knowledge_base_id=? AND tenant_id=? AND deleted_at IS NULL", kb, tenant).Order("id").Limit(2049).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 || len(ids) > 2048 {
			return access.ErrForbidden
		}
		targets := []types.EnterprisePolicyTarget{{Kind: "space", ID: strconv.FormatUint(tenant, 10)}, {Kind: "kb", ID: kb}}
		for _, id := range ids {
			targets = append(targets, types.EnterprisePolicyTarget{Kind: "knowledge", ID: id})
		}
		store := repository.NewEnterpriseAccessRepository(tx)
		for _, target := range targets {
			p, version, owner, err := store.EnterpriseReviewResource(ctx, target)
			if err != nil {
				return err
			}
			if owner != tenant {
				return access.ErrForbidden
			}
			out.Resources = append(out.Resources, PortalReviewResource{Kind: target.Kind, ID: target.ID, Revision: p.Revision, Version: version})
			if target.Kind == "kb" {
				if p.State != "" {
					out.State = p.State
				}
				out.ProposedBy = p.ProposedBy
				out.ReviewedBy = p.ReviewedBy
				out.ReviewedAt = p.ReviewedAt
				out.Reason = p.Reason
				out.InheritNewOrdinary = p.InheritNewOrdinary
				out.ProposerActiveOwner = portalLiveOwner(tx, p.ProposedBy, tenant)
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return out, err
}

func (s *portalService) ChangePublication(ctx context.Context, request PortalReviewRequest) error {
	return s.publicationWorkflow().Apply(ctx, request)
}

type PortalPublicationAudit struct {
	ID          string     `json:"id"`
	ActorUserID string     `json:"actor_user_id"`
	Action      string     `json:"action"`
	TargetType  string     `json:"target_type"`
	TargetID    string     `json:"target_id"`
	CreatedAt   time.Time  `json:"created_at"`
	Details     types.JSON `json:"details"`
}

func (s *portalService) PublicationHistory(ctx context.Context, tenant uint64, kb string) ([]PortalPublicationAudit, error) {
	if _, err := s.PublicationPermissions(ctx, tenant); err != nil {
		return nil, err
	}
	var count int64
	if err := s.db.WithContext(ctx).Table("knowledge_bases").Where("id=? AND tenant_id=? AND deleted_at IS NULL", kb, tenant).Count(&count).Error; err != nil || count != 1 {
		return nil, access.ErrForbidden
	}
	rows := []PortalPublicationAudit{}
	err := s.db.WithContext(ctx).Table("audit_logs").Select("id,actor_user_id,action,target_type,target_id,created_at,details").Where("tenant_id=? AND target_type='enterprise_kb' AND target_id=? AND action IN ?", tenant, kb, []string{"enterprise.policy_propose", "enterprise.policy_approve", "enterprise.policy_revoke", "enterprise.policy_inherit", "enterprise.portal_public_block", "enterprise.portal_public_allow"}).Order("created_at DESC,id DESC").Limit(50).Find(&rows).Error
	return rows, err
}
