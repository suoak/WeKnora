package handler

import (
	"errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func (h *TenantHandler) GetWorkspaceModelDefaults(c *gin.Context) {
	if h.modelPolicy == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Model defaults are unavailable"})
		return
	}
	settings, err := h.modelPolicy.GetWorkspacePolicy(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Cannot load workspace model defaults"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": settings})
}

func (h *TenantHandler) UpdateWorkspaceModelDefaults(c *gin.Context) {
	if h.modelPolicy == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Model defaults are unavailable"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	var req struct {
		Settings *types.DefaultSettings `json:"settings"`
		Previous *types.DefaultSettings `json:"previous"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Settings == nil || req.Previous == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "settings and previous are required"})
		return
	}
	req.Settings.Normalize()
	if err := h.modelPolicy.UpdateWorkspacePolicy(c.Request.Context(), req.Settings, req.Previous); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, types.ErrDefaultSettingsConflict) {
			code = http.StatusConflict
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": req.Settings})
}
