package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/gin-gonic/gin"
)

// FeishuSteadySync requires an ordinary authenticated recovery operator at the
// anchor and independent live management authority for every selected source.
// The controller checks target tenants from its database, never client claims.
func (h *DataSourceHandler) FeishuSteadySync(c *gin.Context) {
	m, actor, ok := h.recoveryActor(c)
	if !ok {
		return
	}
	var request struct {
		Sources           []string                           `json:"sources"`
		AuthorizationDays int                                `json:"authorization_days"`
		Plan              feishusynccontrol.SteadyPlan       `json:"plan"`
		Approval          feishusynccontrol.RecoveryApproval `json:"approval"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(400, gin.H{"error": "invalid_steady_sync_request"})
		return
	}
	switch c.Param("operation") {
	case "plan":
		if request.AuthorizationDays == 0 {
			request.AuthorizationDays = 7
		}
		p, err := m.PlanSteady(c.Request.Context(), actor, request.Sources, request.AuthorizationDays)
		if err != nil {
			c.JSON(409, gin.H{"error": "steady_source_authorization_or_state_rejected"})
			return
		}
		hash, err := feishusynccontrol.RecoveryDigest(p)
		if err != nil {
			c.JSON(500, gin.H{"error": "steady_plan_failed"})
			return
		}
		c.JSON(200, gin.H{"plan": p, "plan_sha256": hash})
	case "activate":
		if err := m.ActivateSteady(c.Request.Context(), request.Plan, request.Approval, actor); err != nil {
			c.JSON(409, gin.H{"error": "steady_activation_rejected"})
			return
		}
		c.JSON(200, gin.H{"sync_queue_kept_paused": true, "approved_sources": len(request.Plan.ManagedSources),
			"policy_revision":        request.Plan.PolicyRevision,
			"authorization_until_ms": request.Plan.AuthorizationUntilMS})
	default:
		c.JSON(400, gin.H{"error": "unsupported_steady_sync_operation"})
	}
}
