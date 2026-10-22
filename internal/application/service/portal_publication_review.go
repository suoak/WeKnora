package service

import (
	"context"
	"strconv"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

type PortalPublicationInventory struct {
	KBID               string `json:"knowledge_base_id"`
	Name               string `json:"name"`
	Documents          int64  `json:"documents"`
	Unready            int64  `json:"unready"`
	ExternalOrUnknown  int64  `json:"external_or_unknown"`
	PolicyState        string `json:"policy_state"`
	PolicyRevision     int64  `json:"policy_revision"`
	InheritNewOrdinary bool   `json:"inherit_new_ordinary"`
	// Policy approval is not live MCP availability. Do not label this public.
	RuntimeStatus string `json:"runtime_status"`
}

// Existing Portal management access authorizes this metadata inventory only.
// It neither implies publication authority nor classifies historical content.
func (s *portalService) PublicationInventory(ctx context.Context, tenant uint64) ([]PortalPublicationInventory, error) {
	rows := []PortalPublicationInventory{}
	q := s.db.WithContext(ctx).Table("knowledge_bases kb").Select(`kb.id AS kb_id,kb.name,
 (SELECT count(*) FROM knowledges k WHERE k.knowledge_base_id=kb.id AND k.tenant_id=kb.tenant_id AND k.deleted_at IS NULL) AS documents,
 (SELECT count(*) FROM knowledges k WHERE k.knowledge_base_id=kb.id AND k.tenant_id=kb.tenant_id AND k.deleted_at IS NULL AND (k.parse_status<>'completed' OR k.enable_status<>'enabled')) AS unready,
 (SELECT count(*) FROM knowledges k WHERE k.knowledge_base_id=kb.id AND k.tenant_id=kb.tenant_id AND k.deleted_at IS NULL AND (COALESCE(k.channel,'') NOT IN ('web','api') OR k.type NOT IN ('file','manual'))) AS external_or_unknown`).Where("kb.tenant_id=? AND kb.deleted_at IS NULL", tenant).Order("kb.id")
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].PolicyState = types.EnterprisePolicyPending
		rows[i].RuntimeStatus = "HOLD_NOT_CLASSIFIED"
	}
	if !s.db.Migrator().HasTable("kb_publication_policies") {
		return rows, nil
	}
	type policy struct {
		KnowledgeBaseID    string
		State              string
		Revision           int64
		InheritNewOrdinary bool
	}
	var policies []policy
	if err := s.db.WithContext(ctx).Table("kb_publication_policies p").Select("p.knowledge_base_id,p.state,p.revision,p.inherit_new_ordinary").Joins("JOIN knowledge_bases kb ON kb.id=p.knowledge_base_id").Where("kb.tenant_id=? AND kb.deleted_at IS NULL", tenant).Find(&policies).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		for _, p := range policies {
			if p.KnowledgeBaseID == rows[i].KBID {
				rows[i].PolicyState = p.State
				rows[i].PolicyRevision = p.Revision
				rows[i].InheritNewOrdinary = p.InheritNewOrdinary
				rows[i].RuntimeStatus = "HOLD_LIVE_PROVIDER_AND_VERSION_VERIFICATION_REQUIRED"
			}
		}
	}
	return rows, nil
}

// PortalFirstConfirmation applies the existing M2 CAS/audit operations in one
// transaction. The caller supplies reviewed resource revisions; no blanket
// public backfill and no implicit Space Owner/SystemAdmin authority exists.
// A formal authority mapping must be injected independently; nil denies.
type PortalFirstConfirmation struct {
	DB               *gorm.DB
	Authority        access.EnterprisePolicyAuthority
	AuthorityFactory func(*gorm.DB) access.EnterprisePolicyAuthority
	RequireVersions  bool
}
type PortalReviewResource struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	Version  string `json:"version"`
}
type PortalReviewRequest struct {
	TenantID           uint64                 `json:"tenant_id"`
	KBID               string                 `json:"knowledge_base_id"`
	Action             string                 `json:"action"`
	Reason             string                 `json:"reason"`
	InheritNewOrdinary bool                   `json:"inherit_new_ordinary"`
	Resources          []PortalReviewResource `json:"resources"`
}

func (s *PortalFirstConfirmation) Apply(ctx context.Context, request PortalReviewRequest) error {
	if s == nil || s.DB == nil || s.Authority == nil && s.AuthorityFactory == nil || request.TenantID == 0 || request.KBID == "" || len(request.Resources) == 0 || len(request.Resources) > 2050 || request.Action != "propose" && request.Action != "approve" && request.Action != "revoke" {
		return access.ErrForbidden
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		authority := s.Authority
		if s.AuthorityFactory != nil {
			authority = s.AuthorityFactory(tx)
		}
		if authority == nil {
			return access.ErrForbidden
		}
		var kb types.KnowledgeBase
		if err := tx.Where("id=? AND tenant_id=? AND is_temporary=false", request.KBID, request.TenantID).Take(&kb).Error; err != nil {
			return access.ErrForbidden
		}
		if kb.Type != "document" {
			return access.ErrForbidden
		}
		if request.Action != "revoke" {
			var published int64
			if err := tx.Table("tenant_portal_configs pc").Joins("JOIN tenants t ON t.id=pc.tenant_id AND t.deleted_at IS NULL AND t.status='active'").Where("pc.tenant_id=? AND pc.status='published'", request.TenantID).Count(&published).Error; err != nil || published != 1 {
				return access.ErrForbidden
			}
		}
		var docs []struct{ ID, Type, Channel, ParseStatus, EnableStatus string }
		if err := tx.Table("knowledges").Select("id,type,channel,parse_status,enable_status").Where("knowledge_base_id=? AND tenant_id=? AND deleted_at IS NULL", kb.ID, kb.TenantID).Find(&docs).Error; err != nil {
			return err
		}
		wanted := map[string]bool{"space:" + strconv.FormatUint(kb.TenantID, 10): true, "kb:" + kb.ID: true}
		for _, d := range docs {
			if request.Action != "revoke" && (d.Type != "file" && d.Type != "manual" || d.Channel != "web" && d.Channel != "api" || d.ParseStatus != "completed" || d.EnableStatus != "enabled") {
				return access.ErrForbidden
			}
			wanted["knowledge:"+d.ID] = true
		}
		if request.Action != "revoke" && len(wanted) != len(request.Resources) {
			return access.ErrForbidden
		}
		store := repository.NewEnterpriseAccessRepository(tx)
		for _, r := range request.Resources {
			k := r.Kind + ":" + r.ID
			if !wanted[k] {
				return access.ErrForbidden
			}
			delete(wanted, k)
			if s.RequireVersions && request.Action != "revoke" {
				p, version, tenant, err := store.EnterpriseReviewResource(ctx, types.EnterprisePolicyTarget{Kind: r.Kind, ID: r.ID})
				if err != nil {
					return err
				}
				if tenant != request.TenantID || r.Version == "" || version != r.Version || p.Revision != r.Revision {
					return types.ErrEnterprisePolicyConflict
				}
				if request.Action == "approve" && !portalLiveOwner(tx, p.ProposedBy, tenant) {
					return access.ErrForbidden
				}
			}
			if request.Action == "propose" && r.Kind == "knowledge" {
				var old types.EnterprisePolicy
				if err := tx.Table("knowledge_access_policies").Where("knowledge_id=?", r.ID).Find(&old).Error; err != nil {
					return err
				}
				if old.Revision > 0 && (old.Policy != "inherit" || old.ContentClass != "ordinary" || old.SourceACLState != "local") {
					return access.ErrForbidden
				}
			}
			_, err := access.ChangeEnterprisePolicy(ctx, store, authority, types.EnterprisePolicyChange{Target: types.EnterprisePolicyTarget{Kind: r.Kind, ID: r.ID}, Action: request.Action, ExpectedRevision: r.Revision, Policy: types.EnterprisePolicyInherit, ContentClass: "ordinary", SourceACLState: "local", InheritNewOrdinary: request.InheritNewOrdinary && r.Kind == "kb", Reason: request.Reason})
			if err != nil {
				return err
			}
		}
		return nil
	})
}
