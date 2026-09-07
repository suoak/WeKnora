package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type portalHandlerServiceStub struct {
	interfaces.PortalService
	spaces             []*types.PortalSpaceResponse
	updateConfigCalled *bool
}

func (s portalHandlerServiceStub) ListSpaces(context.Context, string, uint64, interfaces.PortalListQuery) ([]*types.PortalSpaceResponse, error) {
	return s.spaces, nil
}

func (s portalHandlerServiceStub) UpdateConfig(context.Context, string, uint64, *types.PortalConfigUpdateRequest) (*types.PortalAdminSpaceResponse, error) {
	if s.updateConfigCalled != nil {
		*s.updateConfigCalled = true
	}
	return &types.PortalAdminSpaceResponse{}, nil
}

func TestPortalHandlerSerializationExcludesPrivateFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	role := types.TenantRoleViewer
	h := NewPortalHandler(portalHandlerServiceStub{spaces: []*types.PortalSpaceResponse{{
		TenantID: 8, DisplayName: "Platform Hub", Description: "Discovery only",
		Category: "hardware", ResponsibleTeam: "Platform", Contact: "portal@example.com",
		Stages: []string{"design", "testing"}, Featured: true, AccessState: types.PortalAccessMember,
		CurrentRole: &role, CanRequestAccess: false, InteractionAction: types.PortalInteractionEnter,
	}}})
	r := gin.New()
	r.GET("/portal/spaces", h.ListSpaces)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/portal/spaces?q=platform", nil))
	require.Equal(t, http.StatusOK, w.Code)
	body := strings.ToLower(w.Body.String())
	for _, forbidden := range []string{
		"knowledge_base_count", "knowledge_bases", "knowledge_name", "document_count",
		"document_title", "preview", "chunk", "agent", "mcp", "datasource", "storage",
		"members", "tenant_config", "api_key", "model_config", "interaction_organization_id",
	} {
		require.NotContains(t, body, forbidden)
	}
	require.Contains(t, body, `"interaction_action":"enter"`)
}

func TestPortalAccessRequestBodyRejectsRoleInjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewPortalHandler(portalHandlerServiceStub{})
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.POST("/portal/spaces/:tenant_id/access-requests", h.CreateAccessRequest)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/portal/spaces/8/access-requests",
		strings.NewReader(`{"reason":"please","requested_role":"owner"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPortalConfigPutRejectsStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := false
	h := NewPortalHandler(portalHandlerServiceStub{updateConfigCalled: &called})
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.PUT("/system/admin/portal/spaces/:tenant_id", h.UpdateConfig)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/system/admin/portal/spaces/8",
		strings.NewReader(`{"display_name":"Portal","status":"published"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.False(t, called, "PUT containing status must be rejected before reaching the service")
}
