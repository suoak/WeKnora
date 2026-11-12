package handler

import (
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// ConfigureTenantPersonalContent wires writable target discovery into Key management.
func ConfigureTenantPersonalContent(h *TenantHandler, content *service.PersonalContent) *TenantHandler {
	h.personalContent = content
	return h
}

func (h *TenantHandler) personalContentDetails(c *gin.Context, key *types.TenantAPIKey) gin.H {
	data := userMCPResponse(key)
	data["writable_knowledge_bases"] = []service.WritableKnowledgeBase{}
	if h.personalContent == nil || key.OwnerUserID == nil ||
		c.GetString(types.UserIDContextKey.String()) != *key.OwnerUserID {
		return data
	}
	ctx := c.Request.Context()
	caller := types.CallerFromContext(ctx)
	caller.UserID = *key.OwnerUserID
	ctx = types.WithCaller(ctx, caller)
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: *key.OwnerUserID})
	ctx = types.WithTenantAPIKeyScope(
		ctx,
		types.TenantAPIKeyScope{KeyID: key.ID, ScopeType: types.APIKeyScopeUserMCP, Capabilities: key.Capabilities},
	)
	rows, err := h.personalContent.ListWritable(ctx)
	if err == nil {
		data["writable_knowledge_bases"] = rows
	} else {
		data["writable_scope_unavailable"] = true
	}
	return data
}
