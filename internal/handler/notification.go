package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type NotificationHandler struct {
	repo *repository.NotificationRepository
}

func NewNotificationHandler(db *gorm.DB) *NotificationHandler {
	return &NotificationHandler{repo: repository.NewNotificationRepository(db)}
}

func notificationActor(c *gin.Context) (string, bool) {
	if _, key := types.TenantAPIKeyScopeFromContext(c.Request.Context()); key {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false})
		return "", false
	}
	uid, ok := types.UserIDFromContext(c.Request.Context())
	if !ok || uid == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false})
		return "", false
	}
	return uid, true
}

func (h *NotificationHandler) List(c *gin.Context) {
	uid, ok := notificationActor(c)
	if !ok {
		return
	}
	page, e1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	unread, e3 := strconv.ParseBool(c.DefaultQuery("unread", "false"))
	if e1 != nil || e2 != nil || e3 != nil || page < 1 || page > 1000000 || size < 1 || size > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid pagination or unread filter"})
		return
	}
	rows, total, err := h.repo.List(c.Request.Context(), uid, page, size, unread)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows, "total": total, "page": page, "page_size": size})
}

func (h *NotificationHandler) UnreadCount(c *gin.Context) {
	uid, ok := notificationActor(c)
	if !ok {
		return
	}
	n, err := h.repo.UnreadCount(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"unread_count": n}})
}

func (h *NotificationHandler) MarkRead(c *gin.Context) {
	uid, ok := notificationActor(c)
	if !ok {
		return
	}
	err := h.repo.MarkRead(c.Request.Context(), uid, c.Param("notification_id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
