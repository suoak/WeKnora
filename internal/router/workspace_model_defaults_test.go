package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type workspaceHTTPSettings struct {
	interfaces.SystemSettingService
}

func (*workspaceHTTPSettings) GetString(_ context.Context, key, _, def string) string {
	if key == "model.default.chat_id" {
		return "enterprise-chat"
	}
	return def
}

func TestWorkspaceDefaultsRealHTTPAuthorizationAndInheritance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.Model{}, &types.AuditLog{}))
	require.NoError(t, db.Create(&types.Tenant{ID: 7, Name: "new workspace"}).Error)
	for _, model := range []*types.Model{
		{ID: "enterprise-chat", IsBuiltin: true, Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive},
		{ID: "workspace-chat", TenantID: 7, Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive},
		{ID: "foreign-chat", TenantID: 8, Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive},
	} {
		require.NoError(t, db.Create(model).Error)
	}
	models := repository.NewModelRepository(db)
	policy := service.NewModelPolicyService(&workspaceHTTPSettings{}, models, repository.NewTenantRepository(db))
	modelService := service.NewModelService(models, nil, nil, nil, nil, nil)
	service.AttachModelDefaults(modelService, policy)
	tenantHandler := handler.NewTenantHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, policy)
	enforceRBAC := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enforceRBAC}}
	r := gin.New()
	group := r.Group("/api/v1")
	guards := &rbacGuards{cfg: cfg}
	RegisterTenantRoutes(group, tenantHandler, nil, nil, nil, guards)
	RegisterModelRoutes(group, handler.NewModelHandler(modelService, policy), &handler.ModelCredentialsHandler{}, guards)
	call := func(method, path, body string, role types.TenantRole) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		ctx := context.WithValue(request.Context(), types.TenantIDContextKey, uint64(7))
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
		ctx = context.WithValue(ctx, types.UserIDContextKey, "workspace-admin")
		request = request.WithContext(ctx)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		return response
	}
	const defaults = "/api/v1/tenants/kv/default-settings"
	const effective = "/api/v1/models/default-policy"
	response := call(http.MethodGet, effective, "", types.TenantRoleViewer)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "enterprise-chat")
	body := `{"settings":{"chat_model_id":"workspace-chat"},"previous":{}}`
	response = call(http.MethodPut, defaults, body, types.TenantRoleViewer)
	require.Equal(t, http.StatusForbidden, response.Code)
	response = call(http.MethodPut, defaults, body, types.TenantRoleAdmin)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = call(http.MethodGet, effective, "", types.TenantRoleViewer)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "workspace-chat")
	require.NotContains(t, response.Body.String(), "api_key")
	response = call(http.MethodPut, defaults, body, types.TenantRoleAdmin)
	require.Equal(t, http.StatusConflict, response.Code)
	response = call(http.MethodPut, defaults,
		`{"settings":{"chat_model_id":"foreign-chat"},"previous":{"chat_model_id":"workspace-chat"}}`, types.TenantRoleAdmin)
	require.Equal(t, http.StatusBadRequest, response.Code)
	var count int64
	require.NoError(t, db.Model(&types.AuditLog{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "denied, invalid and conflicting requests cannot write an audit or configuration")
}
