package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNotificationInboxAuthorizationPaginationAndReadState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "inbox.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.UserNotification{}))
	for _, row := range []types.UserNotification{
		{ID: "a1", UserID: "A", NotificationType: "submitted", ReferenceID: "r1", TenantID: 1, CreatedAt: time.Now()},
		{ID: "a2", UserID: "A", NotificationType: "submitted", ReferenceID: "r2", TenantID: 2, CreatedAt: time.Now()},
		{ID: "b1", UserID: "B", NotificationType: "submitted", ReferenceID: "r3", TenantID: 3, CreatedAt: time.Now()},
	} {
		require.NoError(t, db.Create(&row).Error)
	}
	h := NewNotificationHandler(db)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		if uid := c.GetHeader("Test-User"); uid != "" {
			ctx = context.WithValue(ctx, types.UserIDContextKey, uid)
		}
		if c.GetHeader("Test-API-Key") != "" {
			ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{FullAccess: true})
		}
		c.Request = c.Request.WithContext(ctx)
	})
	r.GET("/me/notifications", h.List)
	r.GET("/me/notifications/unread-count", h.UnreadCount)
	r.POST("/me/notifications/:notification_id/read", h.MarkRead)
	r.POST("/me/notifications/read-all", h.MarkRead)
	request := func(method, path, user string, key bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Test-User", user)
		if key {
			req.Header.Set("Test-API-Key", "synthetic")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	require.Equal(t, http.StatusUnauthorized, request("GET", "/me/notifications", "", false).Code)
	require.Equal(t, http.StatusForbidden, request("GET", "/me/notifications", "A", true).Code)
	w := request("GET", "/me/notifications?page=1&page_size=1&user_id=B&tenant_id=3", "A", false)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"total":2`)
	require.NotContains(t, w.Body.String(), `"id":"b1"`)
	require.Equal(t, http.StatusBadRequest, request("GET", "/me/notifications?page_size=101", "A", false).Code)
	require.Equal(t, http.StatusNotFound, request("POST", "/me/notifications/b1/read", "A", false).Code)
	for range 2 {
		require.Equal(t, http.StatusOK, request("POST", "/me/notifications/a1/read", "A", false).Code)
	}
	require.Contains(t, request("GET", "/me/notifications/unread-count", "A", false).Body.String(), `"unread_count":1`)
	require.Contains(t, request("GET", "/me/notifications?unread=true", "A", false).Body.String(), `"total":1`)
	require.Equal(t, http.StatusOK, request("POST", "/me/notifications/read-all", "A", false).Code)
	require.Contains(t, request("GET", "/me/notifications/unread-count", "A", false).Body.String(), `"unread_count":0`)
	require.Contains(t, request("GET", "/me/notifications/unread-count", "B", false).Body.String(), `"unread_count":1`)
	var saved types.UserNotification
	require.NoError(t, db.First(&saved, "id = ?", "a1").Error)
	require.True(t, saved.IsRead)
	require.NotNil(t, saved.ReadAt)
}
