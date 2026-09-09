package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterUsageAnalyticsRoutes(r *gin.RouterGroup, h *handler.UsageAnalyticsHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	// The report endpoint is authenticated by a tenant/user-MCP API key. The
	// handler rejects browser JWT callers and derives attribution server-side.
	g.apiKeyRoute(r, "POST", "/usage/mcp-events", apiKeyAny(), h.ReportInboundMCP)

	admin := r.Group("/system/admin/usage", g.SystemAdmin())
	admin.GET("/overview", h.Overview)
	admin.GET("/tenants", h.Tenants)
	admin.GET("/timeseries", h.TimeSeries)
	admin.GET("/models", h.Models)
	admin.GET("/mcp", h.MCP)
	admin.GET("/knowledge-bases", h.KnowledgeBases)
}
