package router

import (
	"net/http"
	"testing"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPortalRoutesAreJWTOnlyAndDoNotDeclareAPIKeyScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	g := &rbacGuards{}
	engine := gin.New()
	RegisterPortalRoutes(engine.Group("/api/v1"), &handler.PortalHandler{}, g)

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/portal/stages"},
		{http.MethodGet, "/api/v1/portal/spaces"},
		{http.MethodGet, "/api/v1/portal/my-spaces"},
		{http.MethodPost, "/api/v1/portal/spaces/:tenant_id/access-requests"},
		{http.MethodPost, "/api/v1/portal/spaces/:tenant_id/interaction/resolve"},
		{http.MethodGet, "/api/v1/me/tenant-access-requests"},
		{http.MethodPost, "/api/v1/me/tenant-access-requests/:request_id/cancel"},
		{http.MethodGet, "/api/v1/tenants/:id/access-requests"},
		{http.MethodPost, "/api/v1/tenants/:id/access-requests/:request_id/approve"},
		{http.MethodPost, "/api/v1/tenants/:id/access-requests/:request_id/reject"},
		{http.MethodGet, "/api/v1/system/admin/portal/spaces"},
		{http.MethodGet, "/api/v1/system/admin/portal/organization-options"},
		{http.MethodGet, "/api/v1/system/admin/portal/spaces/:tenant_id"},
		{http.MethodPut, "/api/v1/system/admin/portal/spaces/:tenant_id"},
		{http.MethodPost, "/api/v1/system/admin/portal/spaces/:tenant_id/publish"},
		{http.MethodPost, "/api/v1/system/admin/portal/spaces/:tenant_id/unpublish"},
		{http.MethodPost, "/api/v1/system/admin/portal/spaces/:tenant_id/archive"},
	} {
		_, declared := g.ensureAPIKeyAuthorizer().Lookup(route.method, route.path)
		require.Falsef(t, declared, "%s %s must remain JWT-only", route.method, route.path)
	}
	for _, route := range engine.Routes() {
		require.NotEqual(t, "/api/v1/system/admin/portal/spaces/:tenant_id/:action", route.Path,
			"status transitions must use explicit whitelisted routes")
	}
}
