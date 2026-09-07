package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type portalRepository struct{ db *gorm.DB }

func NewPortalRepository(db *gorm.DB) interfaces.PortalRepository {
	return &portalRepository{db: db}
}

func (r *portalRepository) DB() *gorm.DB { return r.db }

type portalConfigRow struct {
	TenantID           uint64
	DisplayName        string
	Description        string
	Category           string
	ResponsibleTeam    string
	Contact            string
	Featured           bool
	AllowAccessRequest bool
	InteractionOrgID   *string `gorm:"column:interaction_organization_id"`
}

func (r *portalRepository) ListPublished(ctx context.Context, userID string, activeTenantID uint64, q interfaces.PortalListQuery) ([]*types.PortalSpaceResponse, error) {
	db := r.db.WithContext(ctx).Table("tenant_portal_configs AS pc").
		Select(`pc.tenant_id, pc.display_name, pc.description, pc.category,
			pc.responsible_team, pc.contact, pc.featured, pc.allow_access_request,
			pc.interaction_organization_id`).
		Joins("JOIN tenants AS t ON t.id = pc.tenant_id AND t.deleted_at IS NULL").
		Where("pc.status = ? AND t.status = ?", types.PortalStatusPublished, "active")
	if q.Query != "" {
		like := "%" + escapeLikePattern(q.Query) + "%"
		db = db.Where(`(LOWER(pc.display_name) LIKE LOWER(?) ESCAPE '\' OR LOWER(pc.description) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(pc.category) LIKE LOWER(?) ESCAPE '\' OR EXISTS (
				SELECT 1 FROM tenant_portal_stages ps
				WHERE ps.tenant_id = pc.tenant_id AND LOWER(ps.stage_key) LIKE LOWER(?) ESCAPE '\'
			))`, like, like, like, like)
	}
	if q.Category != "" {
		db = db.Where("pc.category = ?", q.Category)
	}
	if q.Stage != "" {
		db = db.Where(`EXISTS (SELECT 1 FROM tenant_portal_stages ps
			WHERE ps.tenant_id = pc.tenant_id AND ps.stage_key = ?)`, q.Stage)
	}
	var rows []portalConfigRow
	if err := db.Order("pc.featured DESC, pc.display_order ASC, pc.tenant_id ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []*types.PortalSpaceResponse{}, nil
	}

	tenantIDs := make([]uint64, 0, len(rows))
	for _, row := range rows {
		tenantIDs = append(tenantIDs, row.TenantID)
	}
	stages, err := r.stageMap(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}
	memberships, err := r.membershipMap(ctx, userID, tenantIDs)
	if err != nil {
		return nil, err
	}
	pending, err := r.pendingMap(ctx, userID, tenantIDs)
	if err != nil {
		return nil, err
	}

	interaction := map[string]bool{}
	if activeTenantID != 0 {
		// Organization membership is tenant-level, but an arbitrary/stale
		// tenant id must never grant a user the tenant's organization access.
		// Re-confirm the caller's active workspace membership on every query.
		var activeMembershipCount int64
		if err := r.db.WithContext(ctx).Table("tenant_members AS active_tm").
			Joins("JOIN tenants AS active_t ON active_t.id = active_tm.tenant_id AND active_t.deleted_at IS NULL").
			Where("active_tm.user_id = ? AND active_tm.tenant_id = ? AND active_tm.status = ? AND active_tm.deleted_at IS NULL AND active_t.status = ?",
				userID, activeTenantID, types.TenantMemberStatusActive, "active").
			Count(&activeMembershipCount).Error; err != nil {
			return nil, err
		}
		if activeMembershipCount == 0 {
			activeTenantID = 0
		}
	}
	if activeTenantID != 0 {
		orgIDs := make([]string, 0)
		for _, row := range rows {
			if row.InteractionOrgID != nil && *row.InteractionOrgID != "" {
				orgIDs = append(orgIDs, *row.InteractionOrgID)
			}
		}
		if len(orgIDs) > 0 {
			var found []struct{ OrganizationID string }
			if err := r.db.WithContext(ctx).Table("organization_tenant_members AS otm").
				Select("otm.organization_id").
				Joins("JOIN organizations AS o ON o.id = otm.organization_id AND o.deleted_at IS NULL").
				Where("otm.tenant_id = ? AND otm.organization_id IN ?", activeTenantID, orgIDs).
				Scan(&found).Error; err != nil {
				return nil, err
			}
			for _, item := range found {
				interaction[item.OrganizationID] = true
			}
		}
	}

	out := make([]*types.PortalSpaceResponse, 0, len(rows))
	for _, row := range rows {
		state := types.PortalAccessNotMember
		var currentRole *types.TenantRole
		if member := memberships[row.TenantID]; member != nil {
			switch member.Status {
			case types.TenantMemberStatusActive:
				state = types.PortalAccessMember
				role := member.Role
				currentRole = &role
			case types.TenantMemberStatusSuspended:
				state = types.PortalAccessSuspended
			}
		}
		// Invitations and any future non-authorizing membership states do not
		// outrank a pending portal request. Only active/suspended membership does.
		if state == types.PortalAccessNotMember && pending[row.TenantID] {
			state = types.PortalAccessPending
		}
		action := types.PortalInteractionNone
		if row.InteractionOrgID != nil && interaction[*row.InteractionOrgID] {
			action = types.PortalInteractionEnter
		}
		out = append(out, &types.PortalSpaceResponse{
			TenantID: row.TenantID, DisplayName: row.DisplayName, Description: row.Description,
			Category: row.Category, ResponsibleTeam: row.ResponsibleTeam, Contact: row.Contact,
			Stages: stages[row.TenantID], Featured: row.Featured, AccessState: state,
			CurrentRole:       currentRole,
			CanRequestAccess:  row.AllowAccessRequest && state == types.PortalAccessNotMember,
			InteractionAction: action,
		})
	}
	return out, nil
}

func (r *portalRepository) stageMap(ctx context.Context, tenantIDs []uint64) (map[uint64][]string, error) {
	var rows []types.TenantPortalStage
	if err := r.db.WithContext(ctx).Where("tenant_id IN ?", tenantIDs).
		Order("display_order ASC, stage_key ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint64][]string, len(tenantIDs))
	for _, id := range tenantIDs {
		out[id] = []string{}
	}
	for _, row := range rows {
		out[row.TenantID] = append(out[row.TenantID], row.StageKey)
	}
	return out, nil
}

func (r *portalRepository) membershipMap(ctx context.Context, userID string, tenantIDs []uint64) (map[uint64]*types.TenantMember, error) {
	var rows []*types.TenantMember
	if err := r.db.WithContext(ctx).Where("user_id = ? AND tenant_id IN ?", userID, tenantIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint64]*types.TenantMember, len(rows))
	for _, row := range rows {
		out[row.TenantID] = row
	}
	return out, nil
}

func (r *portalRepository) pendingMap(ctx context.Context, userID string, tenantIDs []uint64) (map[uint64]bool, error) {
	var rows []types.TenantAccessRequest
	if err := r.db.WithContext(ctx).Select("tenant_id").Where(
		"applicant_user_id = ? AND tenant_id IN ? AND status = ?", userID, tenantIDs, types.TenantAccessRequestPending,
	).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint64]bool, len(rows))
	for _, row := range rows {
		out[row.TenantID] = true
	}
	return out, nil
}

func (r *portalRepository) ListMySpaces(ctx context.Context, userID string) ([]*types.PortalMySpaceResponse, error) {
	var rows []*types.PortalMySpaceResponse
	err := r.db.WithContext(ctx).Table("tenant_members AS tm").
		Select("tm.tenant_id, t.name AS tenant_name, tm.role").
		Joins("JOIN tenants AS t ON t.id = tm.tenant_id AND t.deleted_at IS NULL").
		Where("tm.user_id = ? AND tm.status = ? AND tm.deleted_at IS NULL AND t.status = ?", userID, types.TenantMemberStatusActive, "active").
		Order("tm.joined_at ASC, tm.id ASC").Scan(&rows).Error
	return rows, err
}

func (r *portalRepository) ListAdminSpaces(ctx context.Context) ([]*types.PortalAdminSpaceResponse, error) {
	var tenants []*types.Tenant
	if err := r.db.WithContext(ctx).Select("id", "name").Order("created_at DESC").Find(&tenants).Error; err != nil {
		return nil, err
	}
	out := make([]*types.PortalAdminSpaceResponse, 0, len(tenants))
	for _, tenant := range tenants {
		item, err := r.GetAdminSpace(ctx, tenant.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *portalRepository) GetAdminSpace(ctx context.Context, tenantID uint64) (*types.PortalAdminSpaceResponse, error) {
	var tenant types.Tenant
	if err := r.db.WithContext(ctx).Select("id", "name").First(&tenant, tenantID).Error; err != nil {
		return nil, err
	}
	config, stages, err := r.GetConfig(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	item := &types.PortalAdminSpaceResponse{TenantID: tenant.ID, TenantName: tenant.Name, Status: types.PortalStatusDraft, Stages: stages}
	if config != nil {
		item.Status = config.Status
		item.DisplayName = config.DisplayName
		item.Description = config.Description
		item.Category = config.Category
		item.ResponsibleTeam = config.ResponsibleTeam
		item.Contact = config.Contact
		item.Featured = config.Featured
		item.DisplayOrder = config.DisplayOrder
		item.AllowAccessRequest = config.AllowAccessRequest
		item.InteractionOrganizationID = config.InteractionOrganizationID
		item.PublishedAt = config.PublishedAt
		item.CreatedAt = &config.CreatedAt
		item.UpdatedAt = &config.UpdatedAt
	}
	return item, nil
}

func (r *portalRepository) GetConfig(ctx context.Context, tenantID uint64) (*types.TenantPortalConfig, []string, error) {
	var config types.TenantPortalConfig
	err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, []string{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	stages, err := r.stageMap(ctx, []uint64{tenantID})
	return &config, stages[tenantID], err
}

func (r *portalRepository) UpsertConfig(ctx context.Context, config *types.TenantPortalConfig, stages []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing types.TenantPortalConfig
		err := tx.Where("tenant_id = ?", config.TenantID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Config writes always start in draft. Publishing is exclusively
			// handled by SetStatus so its audit cannot be bypassed.
			config.Status = types.PortalStatusDraft
			config.PublishedAt = nil
			if err := tx.Create(config).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			config.CreatedAt = existing.CreatedAt
			config.CreatedBy = existing.CreatedBy
			if err := tx.Model(&types.TenantPortalConfig{}).Where("tenant_id = ?", config.TenantID).Updates(map[string]any{
				"display_name": config.DisplayName, "description": config.Description,
				"category": config.Category, "responsible_team": config.ResponsibleTeam, "contact": config.Contact,
				"featured": config.Featured, "display_order": config.DisplayOrder,
				"allow_access_request":        config.AllowAccessRequest,
				"interaction_organization_id": config.InteractionOrganizationID,
				"updated_by":                  config.UpdatedBy, "updated_at": config.UpdatedAt,
			}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("tenant_id = ?", config.TenantID).Delete(&types.TenantPortalStage{}).Error; err != nil {
			return err
		}
		for i, stage := range stages {
			if err := tx.Create(&types.TenantPortalStage{TenantID: config.TenantID, StageKey: stage, DisplayOrder: i, CreatedAt: time.Now().UTC()}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *portalRepository) SetStatus(ctx context.Context, tenantID uint64, status types.PortalStatus, actor string) (*types.TenantPortalConfig, error) {
	updates := map[string]any{"status": status, "updated_by": actor, "updated_at": time.Now().UTC()}
	if status == types.PortalStatusPublished {
		now := time.Now().UTC()
		updates["published_at"] = &now
	}
	res := r.db.WithContext(ctx).Model(&types.TenantPortalConfig{}).Where("tenant_id = ?", tenantID).Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	config, _, err := r.GetConfig(ctx, tenantID)
	return config, err
}

func (r *portalRepository) CreateAccessRequest(ctx context.Context, request *types.TenantAccessRequest) error {
	return r.db.WithContext(ctx).Create(request).Error
}

func (r *portalRepository) GetPendingAccessRequest(ctx context.Context, tenantID uint64, userID string) (*types.TenantAccessRequest, error) {
	var request types.TenantAccessRequest
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND applicant_user_id = ? AND status = ?", tenantID, userID, types.TenantAccessRequestPending).First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &request, err
}

func (r *portalRepository) GetAccessRequest(ctx context.Context, requestID string, lock bool) (*types.TenantAccessRequest, error) {
	var request types.TenantAccessRequest
	db := r.db.WithContext(ctx).Where("id = ?", requestID)
	if lock && r.db.Dialector.Name() == "postgres" {
		db = db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := db.First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &request, err
}

func (r *portalRepository) ListAccessRequestsByUser(ctx context.Context, userID string) ([]*types.TenantAccessRequest, error) {
	var rows []*types.TenantAccessRequest
	err := r.db.WithContext(ctx).Where("applicant_user_id = ?", userID).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

func (r *portalRepository) ListAccessRequestsByTenant(ctx context.Context, tenantID uint64) ([]*types.TenantAccessRequest, error) {
	var rows []*types.TenantAccessRequest
	err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

func (r *portalRepository) TransitionAccessRequest(ctx context.Context, requestID string, from, to types.TenantAccessRequestStatus, reviewer *string, reviewNote string) (bool, error) {
	now := time.Now().UTC()
	updates := map[string]any{"status": to, "updated_at": now}
	if reviewer != nil {
		updates["reviewed_by"] = *reviewer
		updates["reviewed_at"] = now
		updates["review_note"] = strings.TrimSpace(reviewNote)
	}
	res := r.db.WithContext(ctx).Model(&types.TenantAccessRequest{}).Where("id = ? AND status = ?", requestID, from).Updates(updates)
	return res.RowsAffected == 1, res.Error
}

func (r *portalRepository) HasOrganizationTenantMembership(ctx context.Context, organizationID string, tenantID uint64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Table("organization_tenant_members AS otm").
		Joins("JOIN organizations AS o ON o.id = otm.organization_id AND o.deleted_at IS NULL").
		Where("otm.organization_id = ? AND otm.tenant_id = ?", organizationID, tenantID).Count(&count).Error
	return count > 0, err
}
