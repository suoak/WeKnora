package handler

import (
	"context"
	"net/http"

	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func (h *DataSourceHandler) recoveryActor(c *gin.Context) (*feishusynccontrol.RecoveryManager, string, bool) {
	ctx := c.Request.Context()
	admin, _ := ctx.Value(types.SystemAdminContextKey).(bool)
	actor := types.PrivateKBReadUser(ctx)
	_, keyed := types.TenantAPIKeyScopeFromContext(ctx)
	if !admin || actor == "" || keyed {
		c.JSON(http.StatusForbidden, gin.H{"error": "recovery_operator_forbidden"})
		return nil, "", false
	}
	if _, status, msg := h.getOwnedDataSource(ctx, h.getTenantID(c), c.Param("id")); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return nil, "", false
	}
	m := feishusynccontrol.Recovery()
	if m == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "recovery_not_configured"})
		return nil, "", false
	}
	return m, actor, true
}

// FeishuRecoveryPlan creates a read-only incident plan for an authorized operator.
func (h *DataSourceHandler) FeishuRecoveryPlan(c *gin.Context) {
	m, actor, ok := h.recoveryActor(c)
	if !ok {
		return
	}
	p, err := m.Plan(c.Request.Context(), c.Param("id"), actor)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "recovery_precondition_rejected"})
		return
	}
	hash, err := feishusynccontrol.RecoveryDigest(p)
	if err != nil {
		c.JSON(500, gin.H{"error": "recovery_plan_failed"})
		return
	}
	c.JSON(200, gin.H{"plan": p, "plan_sha256": hash})
}

// FeishuRecoveryStatus reports the requested source operational state.
func (h *DataSourceHandler) FeishuRecoveryStatus(c *gin.Context) {
	m, _, ok := h.recoveryActor(c)
	if !ok {
		return
	}
	state, err := m.Status(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(409, gin.H{"error": "recovery_status_unavailable"})
		return
	}
	c.JSON(200, state)
}

// FeishuRecoveryExecute verifies independent consent before reset or one canary.
func (h *DataSourceHandler) FeishuRecoveryExecute(c *gin.Context) {
	m, actor, ok := h.recoveryActor(c)
	if !ok {
		return
	}
	var request struct {
		Plan     feishusynccontrol.RecoveryPlan     `json:"plan"`
		Approval feishusynccontrol.RecoveryApproval `json:"approval"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if c.ShouldBindJSON(&request) != nil || request.Plan.Facts.SourceID != c.Param("id") {
		c.JSON(400, gin.H{"error": "invalid_recovery_request"})
		return
	}
	var err error
	switch request.Approval.Operation {
	case "reset":
		err = m.Reset(c.Request.Context(), request.Plan, request.Approval, actor)
	case "canary":
		runner, exists := h.service.(interface {
			RunRecoveryCanary(context.Context, string, func(string, int64) error) error
		})
		if !exists {
			c.JSON(503, gin.H{"error": "canary_runner_unavailable"})
			return
		}
		err = m.Canary(
			c.Request.Context(),
			request.Plan,
			request.Approval,
			actor,
			func(ctx context.Context, bind func(string, int64) error) error {
				return runner.RunRecoveryCanary(ctx, c.Param("id"), bind)
			},
		)
	default:
		c.JSON(400, gin.H{"error": "unsupported_recovery_operation"})
		return
	}
	if err != nil {
		c.JSON(409, gin.H{"error": "recovery_incomplete_kept_closed"})
		return
	}
	c.JSON(200, gin.H{"operation": request.Approval.Operation, "sync_queue_kept_paused": true})
}
