package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/gin-gonic/gin"
)

// No API key policies are declared: personal inboxes are JWT-only.
func RegisterNotificationRoutes(r *gin.RouterGroup, h *handler.NotificationHandler) {
	inbox := r.Group("/me/notifications")
	inbox.GET("", h.List)
	inbox.GET("/unread-count", h.UnreadCount)
	inbox.POST("/read-all", h.MarkRead)
	inbox.POST("/:notification_id/read", h.MarkRead)
}
