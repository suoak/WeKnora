package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

func RegisterPortalRoutes(r *gin.RouterGroup, h *handler.PortalHandler, g *rbacGuards) {
	portal := r.Group("/portal")
	{
		portal.GET("/stages", h.ListStages)
		portal.GET("/spaces", h.ListSpaces)
		portal.GET("/my-spaces", h.ListMySpaces)
		portal.POST("/spaces/:tenant_id/access-requests", h.CreateAccessRequest)
		portal.POST("/spaces/:tenant_id/interaction/resolve", h.ResolveInteraction)
	}
	me := r.Group("/me/tenant-access-requests")
	{
		me.GET("", h.ListMyAccessRequests)
		me.POST("/:request_id/cancel", h.CancelAccessRequest)
	}

	// Approval has exactly the same route-level authority as member writes:
	// active workspace Owner plus a matching active/path tenant.
	tenant := r.Group("/tenants/:id", g.PathTenantMatch(), g.Owner())
	{
		tenant.GET("/access-requests", h.ListTenantAccessRequests)
		tenant.POST("/access-requests/:request_id/approve", h.ApproveAccessRequest)
		tenant.POST("/access-requests/:request_id/reject", h.RejectAccessRequest)
	}

	// Raw routes intentionally remain undeclared for API keys. APIKeyGate
	// therefore default-denies them; only JWT SystemAdmin reaches this group.
	admin := r.Group("/system/admin/portal", g.SystemAdmin())
	{
		admin.GET("/organization-options", h.ListOrganizationOptions)
		admin.GET("/spaces", h.ListAdminSpaces)
		admin.GET("/spaces/:tenant_id", h.GetAdminSpace)
		admin.PUT("/spaces/:tenant_id", h.UpdateConfig)
		admin.POST("/spaces/:tenant_id/publish", h.Publish)
		admin.POST("/spaces/:tenant_id/unpublish", h.Unpublish)
		admin.POST("/spaces/:tenant_id/archive", h.Archive)
	}
}
