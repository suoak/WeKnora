package handler

import (
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/gin-gonic/gin"
	"strconv"
)

func (h *FeishuPersonalHandler) Connections(c *gin.Context) {
	rows, err := h.svc.Connections(c.Request.Context())
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, rows)
}
func (h *FeishuPersonalHandler) Resources(c *gin.Context) {
	size := 50
	if raw := c.Query("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			c.JSON(400, gin.H{"error": "invalid_page_size"})
			return
		}
		size = n
	}
	rows, err := h.svc.Resources(c.Request.Context(), service.FeishuResourceQuery{AuthMode: c.Query("auth_mode"), ConnectionID: c.Query("connection_id"), Kind: c.DefaultQuery("kind", "drive"), Parent: c.Query("parent_id"), Space: c.Query("space_id"), Cursor: c.Query("cursor"), Size: size})
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, rows)
}
func (h *FeishuPersonalHandler) CreatePrivateKB(c *gin.Context) {
	var in struct {
		Name string `json:"name"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "invalid_private_kb"})
		return
	}
	kb, err := h.svc.CreatePrivateKnowledgeBase(c.Request.Context(), in.Name)
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(201, kb)
}
