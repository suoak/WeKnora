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
		tenant.GET("/public-knowledge", h.PublicKnowledgeScope)
		tenant.PUT("/public-knowledge/:kb_id/exception", h.SetPublicKnowledgeException)
		tenant.GET("/knowledge-publication", h.PublicationInventory)
		tenant.GET("/knowledge-publication/:kb_id", h.PublicationReviewSnapshot)
		tenant.GET("/knowledge-publication/:kb_id/history", h.PublicationHistory)
		tenant.POST("/knowledge-publication/:kb_id/:action", h.ChangePublication)
	}

	// Raw routes intentionally remain undeclared for API keys. APIKeyGate
	// therefore default-denies them; only JWT SystemAdmin reaches this group.
	admin := r.Group("/system/admin/portal", g.SystemAdmin())
	{
		admin.GET("/organization-options", h.ListOrganizationOptions)
		admin.GET("/spaces", h.ListAdminSpaces)
		admin.GET("/spaces/:tenant_id", h.GetAdminSpace)
		admin.GET("/spaces/:tenant_id/knowledge-publication", h.PublicationInventory)
		admin.GET("/spaces/:tenant_id/public-knowledge", h.PublicKnowledgeScope)
		admin.PUT("/spaces/:tenant_id/public-knowledge/:kb_id/exception", h.SetPublicKnowledgeException)
		admin.GET("/spaces/:tenant_id/knowledge-publication/:kb_id", h.PublicationReviewSnapshot)
		admin.GET("/spaces/:tenant_id/knowledge-publication/:kb_id/history", h.PublicationHistory)
		admin.POST("/spaces/:tenant_id/knowledge-publication/:kb_id/:action", h.ChangePublication)
		admin.PUT("/spaces/:tenant_id", h.UpdateConfig)
		admin.POST("/spaces/:tenant_id/publish", h.Publish)
		admin.POST("/spaces/:tenant_id/unpublish", h.Unpublish)
		admin.POST("/spaces/:tenant_id/archive", h.Archive)
	}
}
