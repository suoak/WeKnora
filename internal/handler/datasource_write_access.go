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

// SourceWriteAccess authorizes workspace members independently of connector
// type, source health, and the knowledge base's original creator.
func (h *DataSourceHandler) SourceWriteAccess(c *gin.Context) {
	ctx := c.Request.Context()
	caller := types.CallerFromContext(ctx)
	if !caller.Role.IsValid() {
		// Outer role/API-key middleware owns machine principals and RBAC rollout.
		// The handler still verifies the source's persisted workspace binding.
		c.Next()
		return
	}
	kbID := ""
	if id := c.Param("id"); id != "" {
		ds, status, message := h.getOwnedDataSource(ctx, h.getTenantID(c), id)
		if status != http.StatusOK {
			c.AbortWithStatusJSON(status, gin.H{"error": message})
			return
		}
		kbID = ds.KnowledgeBaseID
	} else {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_source_request"})
			return
		}
		_ = c.Request.Body.Close()
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		var request struct {
			KnowledgeBaseID string `json:"knowledge_base_id"`
		}
		if json.Unmarshal(body, &request) != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_source_request"})
			return
		}
		kbID = request.KnowledgeBaseID
		if c.FullPath() == "/api/v1/datasource/validate-credentials" {
			c.Next() // Only caller-supplied credentials; no stored source.
			return
		}
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
	if err := access.RequireKBWrite(grant.Context(ctx), kb); err != nil {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "knowledge_base_write_required"})
		return
	}
	c.Request = c.Request.WithContext(grant.Context(ctx))
	c.Next()
}
