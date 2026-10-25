package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/gin-gonic/gin"
)

// RegisterFeishuPersonalRoutes provides the register feishu personal routes operation.
// JWT users only: these routes deliberately do not declare API key policies.
// KB writes depend on the Editor grant; no system/space-admin role is required.
func RegisterFeishuPersonalRoutes(r *gin.RouterGroup, h *handler.FeishuPersonalHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	admin := r.Group("/system/admin/feishu", g.SystemAdmin())
	admin.GET("", h.Config)
	admin.PUT("", h.SaveConfig)
	admin.POST("/test", h.TestConfig)
	me := r.Group("/me/feishu")
	me.GET("", h.Status)
	me.GET("/connections", h.Connections)
	me.GET("/resources", h.Resources)
	me.POST("/private-knowledge-base", h.CreatePrivateKB)
	me.POST("/authorize", h.Authorize)
	me.POST("/callback", h.Complete)
	me.DELETE("", h.Disconnect)
	kb := r.Group("/knowledge-bases/:id/feishu", g.KBAccessWrite("id"))
	kb.GET("", h.List)
	kb.POST("", h.Import)
	kb.POST("/preview", h.Preview)
	kb.POST("/batches", h.CreateBatch)
	kb.GET("/batches", h.ListBatches)
	kb.POST("/sharing-approvals", h.ApproveSharing)
	kb.POST("/imports/:importID/source", h.EnableSource)
	kb.POST("/imports/:importID/source/:action", h.SourceAction)
	batch := r.Group("/feishu/batches")
	batch.GET("/:batchID", h.Batch)
	batch.POST("/:batchID/retry", h.RetryBatch)
	batch.GET("/:batchID/collaboration", h.Collaboration)
	batch.POST("/:batchID/items/:itemID/collaboration", h.Collaborate)
}
