package handler

import (
	"context"
	"net/http"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// FeishuSourceEligibility lets an operator form a batch from the
// independently eligible sources. One unavailable source does not prevent plans
// for other spaces; remote ACL/revision are still checked by the real connector.
func (h *DataSourceHandler) FeishuSourceEligibility(c *gin.Context) {
	m, actor, ok := h.recoveryActor(c)
	if !ok {
		return
	}
	authority, facts, err := m.SourceEligibility(c.Request.Context(), c.Param("id"), actor)
	if err != nil {
		c.JSON(403, gin.H{"error": "source_management_or_local_authority_unavailable"})
		return
	}
	c.JSON(200, gin.H{"authority": authority, "facts": facts, "remote_acl_verified": false})
}

// FeishuSourceReconciliation dispatches only a separately signed reconciliation.
func (h *DataSourceHandler) FeishuSourceReconciliation(c *gin.Context) {
	if _, _, ok := h.recoveryActor(c); !ok {
		return
	}
	var request struct {
		PolicyID string `json:"policy_id"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if c.ShouldBindJSON(&request) != nil || request.PolicyID == "" {
		c.JSON(400, gin.H{"error": "invalid_source_reconciliation_request"})
		return
	}
	s, ok := h.service.(interface {
		RunSourceReconciliation(context.Context, string, string) (*types.SyncLog, error)
	})
	if !ok {
		c.JSON(503, gin.H{"error": "source_reconciliation_unavailable"})
		return
	}
	log, err := s.RunSourceReconciliation(c.Request.Context(), c.Param("id"), request.PolicyID)
	if err != nil {
		c.JSON(409, gin.H{"error": "source_reconciliation_authority_or_state_rejected"})
		return
	}
	c.JSON(202, gin.H{"sync_log_id": log.ID, "shared_queue_kept_paused": true})
}
