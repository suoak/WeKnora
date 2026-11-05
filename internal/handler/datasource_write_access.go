package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// SourceWriteAccess lets ordinary space writers manage Feishu sources in a KB
// they can edit. Existing administrator requirements for other connectors and
// incident recovery controls remain unchanged.
func (h *DataSourceHandler) SourceWriteAccess(c *gin.Context) {
	ctx := c.Request.Context()
	caller := types.CallerFromContext(ctx)
	if !caller.Role.IsValid() {
		// The outer role middleware owns the legacy RBAC rollout switch.
		c.Next()
		return
	}
	if !caller.Role.HasPermission(types.TenantRoleContributor) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "knowledge_base_write_required"})
		return
	}
	isFeishu := func(kind string) bool {
		return kind == "feishu" || kind == "feishu_drive" || kind == "lark" || kind == "lark_drive"
	}
	kind, kbID := "", ""
	if id := c.Param("id"); id != "" {
		ds, status, message := h.getOwnedDataSource(ctx, h.getTenantID(c), id)
		if status != http.StatusOK {
			c.AbortWithStatusJSON(status, gin.H{"error": message})
			return
		}
		kind, kbID = ds.Type, ds.KnowledgeBaseID
		if !caller.Role.HasPermission(types.TenantRoleAdmin) && !isFeishu(kind) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "source_administrator_required"})
			return
		}
		if c.Request.Method == http.MethodPut && c.FullPath() == "/api/v1/datasource/:id" {
			body, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
			if err != nil || len(body) > 1<<20 {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_source_request"})
				return
			}
			_ = c.Request.Body.Close()
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			var update struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(body, &update) != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_source_request"})
				return
			}
			if update.Type != "" {
				kind = update.Type
			}
		}
	} else {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_source_request"})
			return
		}
		_ = c.Request.Body.Close()
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		var request struct {
			Type            string `json:"type"`
			KnowledgeBaseID string `json:"knowledge_base_id"`
		}
		if json.Unmarshal(body, &request) != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_source_request"})
			return
		}
		kind, kbID = request.Type, request.KnowledgeBaseID
	}
	if !caller.Role.HasPermission(types.TenantRoleAdmin) && !isFeishu(kind) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "source_administrator_required"})
		return
	}
	if c.Param("id") == "" && c.FullPath() == "/api/v1/datasource/validate-credentials" {
		// Tests only the caller-supplied credentials; no stored source is read.
		c.Next()
		return
	}
	kb, status, message := h.getOwnedKnowledgeBase(ctx, h.getTenantID(c), kbID)
	if status != http.StatusOK {
		c.AbortWithStatusJSON(status, gin.H{"error": message})
		return
	}
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: caller}, kb, types.OrgRoleEditor, nil, nil)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "knowledge_base_write_required"})
		return
	}
	c.Request = c.Request.WithContext(grant.Context(ctx))
	c.Next()
}
