package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var (
	ErrPortalNotFound             = errors.New("portal space not found")
	ErrPortalNotRequestable       = errors.New("portal space does not accept access requests")
	ErrPortalInvalidReason        = errors.New("reason must be between 1 and 1000 characters")
	ErrPortalInvalidConfig        = errors.New("invalid portal configuration")
	ErrPortalInvalidCategory      = errors.New("category must be distinct from IPD stage keys")
	ErrPortalInvalidStage         = errors.New("invalid IPD stage")
	ErrPortalPendingExists        = errors.New("a pending access request already exists")
	ErrPortalAlreadyMember        = errors.New("user is already an active workspace member")
	ErrPortalMembershipSuspended  = errors.New("workspace membership is suspended")
	ErrPortalRequestNotFound      = errors.New("access request not found")
	ErrPortalRequestNotPending    = errors.New("access request is not pending")
	ErrPortalApplicantUnavailable = errors.New("access request applicant is not an active user")
	ErrPortalReviewForbidden      = errors.New("active workspace owner membership required")
	ErrPortalInteractionForbidden = errors.New("interaction space is not available to the active workspace")
)

type portalService struct {
	db        *gorm.DB
	repo      interfaces.PortalRepository
	tenantSvc interfaces.TenantService
	memberSvc interfaces.TenantMemberService
	orgRepo   interfaces.OrganizationRepository
	audit     interfaces.AuditLogService
}

func NewPortalService(
	db *gorm.DB,
	repo interfaces.PortalRepository,
	tenantSvc interfaces.TenantService,
	memberSvc interfaces.TenantMemberService,
	orgRepo interfaces.OrganizationRepository,
	audit interfaces.AuditLogService,
) interfaces.PortalService {
	return &portalService{db: db, repo: repo, tenantSvc: tenantSvc, memberSvc: memberSvc, orgRepo: orgRepo, audit: audit}
}

func (s *portalService) ListStages() []*types.PortalStageResponse {
	out := make([]*types.PortalStageResponse, 0, len(types.BuiltinPortalStages))
	for i, key := range types.BuiltinPortalStages {
		out = append(out, &types.PortalStageResponse{Key: key, DisplayOrder: i})
	}
	return out
}

func normalizePortalCategory(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len([]rune(value)) > 64 {
		return "", ErrPortalInvalidConfig
	}
	if value != "" && types.IsBuiltinPortalStage(value) {
		return "", ErrPortalInvalidCategory
	}
	return value, nil
}

func normalizePortalStage(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !types.IsBuiltinPortalStage(value) {
		return "", ErrPortalInvalidStage
	}
	return value, nil
}

func (s *portalService) ListSpaces(ctx context.Context, userID string, activeTenantID uint64, q interfaces.PortalListQuery) ([]*types.PortalSpaceResponse, error) {
	q.Query = strings.TrimSpace(q.Query)
	if len([]rune(q.Query)) > 128 {
		return nil, ErrPortalInvalidConfig
	}
	category, err := normalizePortalCategory(q.Category)
	if err != nil {
		return nil, err
	}
	q.Category = category
	if strings.TrimSpace(q.Stage) != "" {
		q.Stage, err = normalizePortalStage(q.Stage)
		if err != nil {
			return nil, err
		}
	}
	return s.repo.ListPublished(ctx, userID, activeTenantID, q)
}

func (s *portalService) ListMySpaces(ctx context.Context, userID string) ([]*types.PortalMySpaceResponse, error) {
	return s.repo.ListMySpaces(ctx, userID)
}

func tenantIsActive(tenant *types.Tenant) bool {
	return tenant != nil && strings.EqualFold(strings.TrimSpace(tenant.Status), "active") && !tenant.DeletedAt.Valid
}

func (s *portalService) CreateAccessRequest(ctx context.Context, userID string, tenantID uint64, reason string) (*types.TenantAccessRequest, error) {
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) < 1 || len([]rune(reason)) > 1000 {
		return nil, ErrPortalInvalidReason
	}
	tenant, err := s.tenantSvc.GetTenantByID(ctx, tenantID)
	if err != nil || !tenantIsActive(tenant) {
		return nil, ErrPortalNotFound
	}
	config, _, err := s.repo.GetConfig(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if config == nil || config.Status != types.PortalStatusPublished || !config.AllowAccessRequest {
		return nil, ErrPortalNotRequestable
	}
	member, err := s.memberSvc.GetMembership(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}
	if member != nil {
		switch member.Status {
		case types.TenantMemberStatusActive:
			return nil, ErrPortalAlreadyMember
		case types.TenantMemberStatusSuspended:
			return nil, ErrPortalMembershipSuspended
		}
	}
	if pending, err := s.repo.GetPendingAccessRequest(ctx, tenantID, userID); err != nil {
		return nil, err
	} else if pending != nil {
		return nil, ErrPortalPendingExists
	}

	request := &types.TenantAccessRequest{
		ID: uuid.NewString(), TenantID: tenantID, ApplicantUserID: userID,
		Source: "portal", Status: types.TenantAccessRequestPending, Reason: reason,
		RequestedRole: types.TenantRoleViewer, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewPortalRepository(tx)
		if err := txRepo.CreateAccessRequest(ctx, request); err != nil {
			if isDuplicateMembership(err) {
				return ErrPortalPendingExists
			}
			return err
		}
		return NewAuditLogService(repository.NewAuditLogRepository(tx)).Log(ctx, &types.AuditLog{
			TenantID: tenantID, ActorUserID: userID, ActorRole: "applicant",
			Action: types.AuditActionTenantAccessRequested, TargetType: "tenant_access_request",
			TargetID: request.ID, TargetUserID: userID, Outcome: types.AuditOutcomeSuccess,
			Details: portalAuditDetails(request.ID, "portal"),
		})
	})
	if err != nil {
		return nil, err
	}
	return request, nil
}

func (s *portalService) ListMyAccessRequests(ctx context.Context, userID string) ([]*types.TenantAccessRequest, error) {
	return s.repo.ListAccessRequestsByUser(ctx, userID)
}

func (s *portalService) CancelAccessRequest(ctx context.Context, userID, requestID string) error {
	request, err := s.repo.GetAccessRequest(ctx, requestID, false)
	if err != nil {
		return err
	}
	if request == nil || request.ApplicantUserID != userID {
		return ErrPortalRequestNotFound
	}
	if request.Status != types.TenantAccessRequestPending {
		return ErrPortalRequestNotPending
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewPortalRepository(tx)
		changed, err := txRepo.TransitionAccessRequest(ctx, requestID, types.TenantAccessRequestPending, types.TenantAccessRequestCancelled, nil, "")
		if err != nil {
			return err
		}
		if !changed {
			return ErrPortalRequestNotPending
		}
		return NewAuditLogService(repository.NewAuditLogRepository(tx)).Log(ctx, &types.AuditLog{
			TenantID: request.TenantID, ActorUserID: userID, ActorRole: "applicant",
			Action: types.AuditActionTenantAccessRequestCancelled, TargetType: "tenant_access_request",
			TargetID: request.ID, TargetUserID: userID, Outcome: types.AuditOutcomeSuccess,
		})
	})
}

func (s *portalService) ListTenantAccessRequests(ctx context.Context, reviewerID string, tenantID uint64) ([]*types.TenantAccessRequest, error) {
	if err := s.requireActiveOwner(ctx, s.memberSvc, reviewerID, tenantID); err != nil {
		return nil, err
	}
	tenant, err := s.tenantSvc.GetTenantByID(ctx, tenantID)
	if err != nil || !tenantIsActive(tenant) {
		return nil, ErrPortalNotFound
	}
	return s.repo.ListAccessRequestsByTenant(ctx, tenantID)
}

func (s *portalService) requireActiveOwner(ctx context.Context, memberSvc interfaces.TenantMemberService, reviewerID string, tenantID uint64) error {
	member, err := memberSvc.GetMembership(ctx, reviewerID, tenantID)
	if err != nil {
		return err
	}
	if member == nil || member.Status != types.TenantMemberStatusActive || member.Role != types.TenantRoleOwner {
		return ErrPortalReviewForbidden
	}
	return nil
}

func (s *portalService) ApproveAccessRequest(ctx context.Context, reviewerID string, tenantID uint64, requestID, note string) error {
	return s.reviewAccessRequest(ctx, reviewerID, tenantID, requestID, note, true)
}

func (s *portalService) RejectAccessRequest(ctx context.Context, reviewerID string, tenantID uint64, requestID, note string) error {
	return s.reviewAccessRequest(ctx, reviewerID, tenantID, requestID, note, false)
}

func (s *portalService) reviewAccessRequest(ctx context.Context, reviewerID string, tenantID uint64, requestID, note string, approve bool) error {
	if len([]rune(strings.TrimSpace(note))) > 1000 {
		return ErrPortalInvalidConfig
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewPortalRepository(tx)
		txAudit := NewAuditLogService(repository.NewAuditLogRepository(tx))
		txMemberSvc := NewTenantMemberService(repository.NewTenantMemberRepository(tx), txAudit, nil, nil)
		if err := s.requireActiveOwner(ctx, txMemberSvc, reviewerID, tenantID); err != nil {
			return err
		}
		var tenant types.Tenant
		if err := tx.WithContext(ctx).First(&tenant, tenantID).Error; err != nil || !tenantIsActive(&tenant) {
			return ErrPortalNotFound
		}
		request, err := txRepo.GetAccessRequest(ctx, requestID, true)
		if err != nil {
			return err
		}
		if request == nil || request.TenantID != tenantID {
			return ErrPortalRequestNotFound
		}
		targetStatus := types.TenantAccessRequestRejected
		action := types.AuditActionTenantAccessRequestRejected
		if approve {
			targetStatus = types.TenantAccessRequestApproved
			action = types.AuditActionTenantAccessRequestApproved
		}
		if request.Status == targetStatus {
			return nil
		}
		if request.Status != types.TenantAccessRequestPending {
			return ErrPortalRequestNotPending
		}
		if approve {
			var applicant types.User
			if err := tx.WithContext(ctx).Select("id").Where(
				"id = ? AND is_active = ?", request.ApplicantUserID, true,
			).First(&applicant).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrPortalApplicantUnavailable
				}
				return err
			}
			member, err := txMemberSvc.GetMembership(ctx, request.ApplicantUserID, tenantID)
			if err != nil {
				return err
			}
			if member != nil && member.Status == types.TenantMemberStatusSuspended {
				return ErrPortalMembershipSuspended
			}
			if member == nil || member.Status != types.TenantMemberStatusActive {
				if _, err := txMemberSvc.AddMember(ctx, request.ApplicantUserID, tenantID, types.TenantRoleViewer, &reviewerID); err != nil {
					if !errors.Is(err, ErrMembershipAlreadyExists) {
						return err
					}
					winner, getErr := txMemberSvc.GetMembership(ctx, request.ApplicantUserID, tenantID)
					if getErr != nil || winner == nil || winner.Status != types.TenantMemberStatusActive {
						return err
					}
				}
			}
		}
		changed, err := txRepo.TransitionAccessRequest(ctx, requestID, types.TenantAccessRequestPending, targetStatus, &reviewerID, note)
		if err != nil {
			return err
		}
		if !changed {
			fresh, getErr := txRepo.GetAccessRequest(ctx, requestID, false)
			if getErr == nil && fresh != nil && fresh.Status == targetStatus {
				return nil
			}
			return ErrPortalRequestNotPending
		}
		return txAudit.Log(ctx, &types.AuditLog{
			TenantID: tenantID, ActorUserID: reviewerID, ActorRole: string(types.TenantRoleOwner),
			Action: action, TargetType: "tenant_access_request", TargetID: request.ID,
			TargetUserID: request.ApplicantUserID, Outcome: types.AuditOutcomeSuccess,
			Details: portalAuditDetails(request.ID, "portal"),
		})
	})
}

func (s *portalService) ResolveInteraction(ctx context.Context, userID string, activeTenantID, portalTenantID uint64) (string, error) {
	if activeTenantID == 0 {
		return "", ErrPortalInteractionForbidden
	}
	tenant, err := s.tenantSvc.GetTenantByID(ctx, portalTenantID)
	if err != nil || !tenantIsActive(tenant) {
		return "", ErrPortalNotFound
	}
	config, _, err := s.repo.GetConfig(ctx, portalTenantID)
	if err != nil {
		return "", err
	}
	if config == nil || config.Status != types.PortalStatusPublished || config.InteractionOrganizationID == nil {
		return "", ErrPortalInteractionForbidden
	}
	member, err := s.memberSvc.GetMembership(ctx, userID, activeTenantID)
	if err != nil || member == nil || member.Status != types.TenantMemberStatusActive {
		return "", ErrPortalInteractionForbidden
	}
	if _, err := s.orgRepo.GetByID(ctx, *config.InteractionOrganizationID); err != nil {
		return "", ErrPortalInteractionForbidden
	}
	allowed, err := s.repo.HasOrganizationTenantMembership(ctx, *config.InteractionOrganizationID, activeTenantID)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", ErrPortalInteractionForbidden
	}
	return "/platform/organizations?organization_id=" + url.QueryEscape(*config.InteractionOrganizationID), nil
}

func (s *portalService) ListAdminSpaces(ctx context.Context) ([]*types.PortalAdminSpaceResponse, error) {
	return s.repo.ListAdminSpaces(ctx)
}

func (s *portalService) GetAdminSpace(ctx context.Context, tenantID uint64) (*types.PortalAdminSpaceResponse, error) {
	item, err := s.repo.GetAdminSpace(ctx, tenantID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPortalNotFound
	}
	return item, err
}

func (s *portalService) UpdateConfig(ctx context.Context, actorID string, tenantID uint64, req *types.PortalConfigUpdateRequest) (*types.PortalAdminSpaceResponse, error) {
	if req == nil {
		return nil, ErrPortalInvalidConfig
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if len([]rune(displayName)) < 1 || len([]rune(displayName)) > 128 || len([]rune(req.Description)) > 4000 || len([]rune(req.ResponsibleTeam)) > 128 || len([]rune(req.Contact)) > 256 {
		return nil, ErrPortalInvalidConfig
	}
	category, err := normalizePortalCategory(req.Category)
	if err != nil {
		return nil, err
	}
	stages := make([]string, 0, len(req.Stages))
	seen := map[string]bool{}
	for _, raw := range req.Stages {
		stage, err := normalizePortalStage(raw)
		if err != nil {
			return nil, err
		}
		if !seen[stage] {
			seen[stage] = true
			stages = append(stages, stage)
		}
	}
	if req.InteractionOrganizationID != nil {
		id := strings.TrimSpace(*req.InteractionOrganizationID)
		if id == "" {
			req.InteractionOrganizationID = nil
		} else {
			if _, err := s.orgRepo.GetByID(ctx, id); err != nil {
				return nil, ErrPortalInvalidConfig
			}
			req.InteractionOrganizationID = &id
		}
	}
	tenant, err := s.tenantSvc.GetTenantByID(ctx, tenantID)
	if err != nil || !tenantIsActive(tenant) {
		return nil, ErrPortalNotFound
	}
	old, _, err := s.repo.GetConfig(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	publishedAt := (*time.Time)(nil)
	createdBy := actorID
	if old != nil {
		createdBy = old.CreatedBy
		publishedAt = old.PublishedAt
	}
	status := types.PortalStatusDraft
	if old != nil {
		status = old.Status
	}
	config := &types.TenantPortalConfig{
		TenantID: tenantID, Status: status, DisplayName: displayName,
		Description: strings.TrimSpace(req.Description), Category: category,
		ResponsibleTeam: strings.TrimSpace(req.ResponsibleTeam), Contact: strings.TrimSpace(req.Contact),
		Featured: req.Featured, DisplayOrder: req.DisplayOrder, AllowAccessRequest: req.AllowAccessRequest,
		InteractionOrganizationID: req.InteractionOrganizationID, CreatedBy: createdBy, UpdatedBy: actorID,
		PublishedAt: publishedAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.UpsertConfig(ctx, config, stages); err != nil {
		return nil, err
	}
	s.emitPortalConfigAudit(ctx, actorID, tenantID)
	return s.repo.GetAdminSpace(ctx, tenantID)
}

func (s *portalService) SetStatus(ctx context.Context, actorID string, tenantID uint64, status types.PortalStatus) (*types.PortalAdminSpaceResponse, error) {
	if !status.IsValid() {
		return nil, ErrPortalInvalidConfig
	}
	tenant, err := s.tenantSvc.GetTenantByID(ctx, tenantID)
	if err != nil || !tenantIsActive(tenant) {
		return nil, ErrPortalNotFound
	}
	old, _, err := s.repo.GetConfig(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, ErrPortalNotFound
	}
	if old.Status != status {
		if _, err := s.repo.SetStatus(ctx, tenantID, status, actorID); err != nil {
			return nil, err
		}
		s.emitPortalStatusAudit(ctx, actorID, tenantID, status)
	}
	return s.repo.GetAdminSpace(ctx, tenantID)
}

func portalAuditDetails(id, source string) types.JSON {
	b, _ := json.Marshal(map[string]string{"request_id": id, "source": source})
	return types.JSON(b)
}

func (s *portalService) emitPortalConfigAudit(ctx context.Context, actorID string, tenantID uint64) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Log(ctx, &types.AuditLog{TenantID: 0, ActorUserID: actorID, ActorRole: "system_admin", Action: types.AuditActionPortalConfigUpdated, ScopeType: "tenant", ScopeID: fmt.Sprint(tenantID), Outcome: types.AuditOutcomeSuccess})
}

func (s *portalService) emitPortalStatusAudit(ctx context.Context, actorID string, tenantID uint64, status types.PortalStatus) {
	if s.audit == nil {
		return
	}
	action := types.AuditActionPortalUnpublished
	switch status {
	case types.PortalStatusPublished:
		action = types.AuditActionPortalPublished
	case types.PortalStatusArchived:
		action = types.AuditActionPortalArchived
	}
	_ = s.audit.Log(ctx, &types.AuditLog{TenantID: 0, ActorUserID: actorID, ActorRole: "system_admin", Action: action, ScopeType: "tenant", ScopeID: fmt.Sprint(tenantID), Outcome: types.AuditOutcomeSuccess})
}
