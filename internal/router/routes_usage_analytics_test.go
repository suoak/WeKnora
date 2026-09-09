package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type usageAnalyticsServiceStub struct {
	interfaces.UsageAnalyticsService
}

func (*usageAnalyticsServiceStub) Overview(context.Context, types.UsageTimeRange) (*types.UsageOverview, error) {
	return &types.UsageOverview{TotalTokens: 12}, nil
}

func TestUsageAnalyticsRoutesRequireSystemAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	v1 := engine.Group("/api/v1")
	RegisterUsageAnalyticsRoutes(v1, handler.NewUsageAnalyticsHandler(&usageAnalyticsServiceStub{}), &rbacGuards{})

	normal := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/admin/usage/overview", nil)
	engine.ServeHTTP(normal, request)
	require.Equal(t, http.StatusForbidden, normal.Code)

	adminEngine := gin.New()
	adminEngine.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.SystemAdminContextKey, true))
		c.Next()
	})
	adminV1 := adminEngine.Group("/api/v1")
	RegisterUsageAnalyticsRoutes(adminV1, handler.NewUsageAnalyticsHandler(&usageAnalyticsServiceStub{}), &rbacGuards{})
	admin := httptest.NewRecorder()
	adminEngine.ServeHTTP(admin, httptest.NewRequest(http.MethodGet, "/api/v1/system/admin/usage/overview", nil))
	require.Equal(t, http.StatusOK, admin.Code)
	require.Contains(t, admin.Body.String(), `"total_tokens":12`)
}
