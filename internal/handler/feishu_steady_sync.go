package handler

import (
	"net/http"
	"time"

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
		Sources             []string                           `json:"sources"`
		ReconcileSources    []string                           `json:"reconcile_sources"`
		AuthorizationDays   int                                `json:"authorization_days"`
		ReconciliationHours int                                `json:"reconciliation_hours"`
		ReleaseSharedQueue  bool                               `json:"release_shared_queue"`
		Plan                feishusynccontrol.SteadyPlan       `json:"plan"`
		Approval            feishusynccontrol.RecoveryApproval `json:"approval"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(400, gin.H{"error": "invalid_steady_sync_request"})
		return
	}
	switch c.Param("operation") {
	case "plan":
		if request.ReconciliationHours < 0 || request.ReconciliationHours > 12 {
			c.JSON(400, gin.H{"error": "invalid_reconciliation_window"})
			return
		}
		if request.ReconciliationHours == 0 {
			request.ReconciliationHours = 1
		}
		window := time.Duration(request.ReconciliationHours) * time.Hour
		if request.AuthorizationDays == 0 {
			request.AuthorizationDays = 7
		}
		var p feishusynccontrol.SteadyPlan
		var err error
		if request.ReleaseSharedQueue {
			p, err = m.PlanSteadyWithSharedQueueRelease(c.Request.Context(), actor,
				request.Sources, request.ReconcileSources, request.AuthorizationDays, window)
		} else {
			p, err = m.PlanSteadyWithReconciliation(c.Request.Context(), actor,
				request.Sources, request.ReconcileSources, request.AuthorizationDays, window)
		}
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
		c.JSON(200, gin.H{
			"sync_queue_kept_paused": !request.Plan.ReleaseSharedQueue,
			"approved_sources":       len(request.Plan.ManagedSources),
			"policy_revision":        request.Plan.PolicyRevision,
			"authorization_until_ms": request.Plan.AuthorizationUntilMS,
		})
	default:
		c.JSON(400, gin.H{"error": "unsupported_steady_sync_operation"})
	}
}
