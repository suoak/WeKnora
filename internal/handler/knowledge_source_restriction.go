package handler

import (
	"context"
	"net/http"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// GetKnowledgeBaseSourceRestriction returns only a current admission verdict.
// The existing owner-tenant metadata path retains private-owner checks; neither
// system administrator status nor a machine/personal MCP Key bypasses them.
func (h *DataSourceHandler) GetKnowledgeBaseSourceRestriction(c *gin.Context) {
	ctx := c.Request.Context()
	principal, principalOK := types.PrincipalFromContext(ctx)
	user, userOK := types.UserIDFromContext(ctx)
	caller := types.CallerFromContext(ctx)
	tenant := h.getTenantID(c)
	if !principalOK || principal.Type != types.PrincipalWebUser || !userOK || principal.ID != user ||
		tenant == 0 || caller.TenantID != tenant || caller.UserID != user ||
		!caller.Role.HasPermission(types.TenantRoleViewer) {
		c.JSON(http.StatusForbidden, gin.H{"error": "source_status_forbidden"})
		return
	}
	// Keep metadata authorization provenance separate from content-response
	// provenance. Private-owner checks still run; no document/content read
	// exception is installed and this context is never put on the request.
	metadataCtx := types.WithLegacyReadScope(ctx)
	kb, status, message := h.getOwnedKnowledgeBase(metadataCtx, tenant, c.Param("id"))
	if status != http.StatusOK {
		c.JSON(status, gin.H{"error": message})
		return
	}
	reader, ok := h.service.(interface {
		KnowledgeBaseSourceRestriction(context.Context, uint64, string) (types.KnowledgeSourceRestriction, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "source_status_unavailable"})
		return
	}
	verdict, err := reader.KnowledgeBaseSourceRestriction(ctx, tenant, kb.ID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "source_status_unavailable"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": verdict})
}
