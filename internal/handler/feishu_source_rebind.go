package handler

import (
	"net/http"
	"os"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// SourceRebind is administrative, never an ordinary user's import action.
// Mutation is disabled until separately enabled by the operator after approval.
// Prepare is read-only. No action can release hold or a global stop.
func (h *FeishuPersonalHandler) SourceRebind(c *gin.Context) {
	ctx := c.Request.Context()
	if !types.IsSystemAdminFromContext(ctx) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	action := c.Param("action")
	if action != "prepare" && os.Getenv("FEISHU_SOURCE_REBIND_OPERATIONS_ENABLED") != "1" {
		c.JSON(http.StatusForbidden, gin.H{"error": "source_rebind_operations_disabled"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var in struct {
		Sources          []string  `json:"source_ids"`
		Proposal         string    `json:"proposal"`
		GrantID          string    `json:"publication_grant_id"`
		ReceiptID        string    `json:"receipt_id"`
		AuthorizationRef string    `json:"authorization_ref"`
		BackupSHA        string    `json:"verified_backup_sha256"`
		ExpiresAt        time.Time `json:"expires_at"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_rebind_request"})
		return
	}
	operator, ok := types.UserIDFromContext(ctx)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_session_required"})
		return
	}
	var value string
	var err error
	switch action {
	case "prepare":
		value, err = h.svc.PrepareSourceRebind(ctx, in.Sources, operator)
	case "authorize-publication":
		value, err = h.svc.AuthorizeSourceRebindPublication(
			ctx,
			in.Proposal,
			operator,
			in.AuthorizationRef,
			in.ExpiresAt,
		)
	case "apply":
		value, err = h.svc.ApplySourceRebind(
			ctx,
			in.Proposal,
			in.GrantID,
			operator,
			in.AuthorizationRef,
			in.BackupSHA,
		)
	case "rollback":
		err = h.svc.RollbackSourceRebind(ctx, in.ReceiptID, operator, in.AuthorizationRef)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_rebind_action"})
		return
	}
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "source_rebind_precondition_failed"})
		return
	}
	c.JSON(
		http.StatusOK,
		gin.H{"operation": action, "result": value, "hold_released": false, "stop_reset": false},
	)
}
