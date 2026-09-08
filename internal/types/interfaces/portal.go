package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

type PortalListQuery struct {
	Query    string
	Stage    string
	Category string
}

type PortalRepository interface {
	ListPublished(ctx context.Context, userID string, activeTenantID uint64, q PortalListQuery) ([]*types.PortalSpaceResponse, error)
	ListMySpaces(ctx context.Context, userID string) ([]*types.PortalMySpaceResponse, error)
	ListAdminSpaces(ctx context.Context) ([]*types.PortalAdminSpaceResponse, error)
	ListOrganizationOptions(ctx context.Context) ([]*types.PortalOrganizationOption, error)
	GetAdminSpace(ctx context.Context, tenantID uint64) (*types.PortalAdminSpaceResponse, error)
	GetConfig(ctx context.Context, tenantID uint64) (*types.TenantPortalConfig, []string, error)
	UpsertConfig(ctx context.Context, config *types.TenantPortalConfig, stages []string) error
	SetStatus(ctx context.Context, tenantID uint64, status types.PortalStatus, actor string) (*types.TenantPortalConfig, error)
	CreateAccessRequest(ctx context.Context, request *types.TenantAccessRequest) error
	GetPendingAccessRequest(ctx context.Context, tenantID uint64, userID string) (*types.TenantAccessRequest, error)
	GetAccessRequest(ctx context.Context, requestID string, lock bool) (*types.TenantAccessRequest, error)
	ListAccessRequestsByUser(ctx context.Context, userID string) ([]*types.TenantAccessRequest, error)
	ListAccessRequestsByTenant(ctx context.Context, tenantID uint64) ([]*types.TenantAccessRequest, error)
	TransitionAccessRequest(ctx context.Context, requestID string, from, to types.TenantAccessRequestStatus, reviewer *string, reviewNote string) (bool, error)
	HasOrganizationTenantMembership(ctx context.Context, organizationID string, tenantID uint64) (bool, error)
	DB() *gorm.DB
}

type PortalService interface {
	ListStages() []*types.PortalStageResponse
	ListSpaces(ctx context.Context, userID string, activeTenantID uint64, q PortalListQuery) ([]*types.PortalSpaceResponse, error)
	ListMySpaces(ctx context.Context, userID string) ([]*types.PortalMySpaceResponse, error)
	CreateAccessRequest(ctx context.Context, userID string, tenantID uint64, reason string) (*types.TenantAccessRequest, error)
	ListMyAccessRequests(ctx context.Context, userID string) ([]*types.TenantAccessRequest, error)
	CancelAccessRequest(ctx context.Context, userID, requestID string) error
	ListTenantAccessRequests(ctx context.Context, reviewerID string, tenantID uint64) ([]*types.TenantAccessRequest, error)
	ApproveAccessRequest(ctx context.Context, reviewerID string, tenantID uint64, requestID, note string) error
	RejectAccessRequest(ctx context.Context, reviewerID string, tenantID uint64, requestID, note string) error
	ResolveInteraction(ctx context.Context, userID string, activeTenantID, portalTenantID uint64) (string, error)
	ListAdminSpaces(ctx context.Context) ([]*types.PortalAdminSpaceResponse, error)
	ListOrganizationOptions(ctx context.Context) ([]*types.PortalOrganizationOption, error)
	GetAdminSpace(ctx context.Context, tenantID uint64) (*types.PortalAdminSpaceResponse, error)
	UpdateConfig(ctx context.Context, actorID string, tenantID uint64, req *types.PortalConfigUpdateRequest) (*types.PortalAdminSpaceResponse, error)
	SetStatus(ctx context.Context, actorID string, tenantID uint64, status types.PortalStatus) (*types.PortalAdminSpaceResponse, error)
}
