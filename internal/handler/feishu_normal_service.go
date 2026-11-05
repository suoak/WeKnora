package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/gin-gonic/gin"
)

// FeishuNormalService is a one-time service-boundary operation. Ordinary source
// creation, scheduling and retries never call this administrator endpoint.
func (h *DataSourceHandler) FeishuNormalService(c *gin.Context) {
	m, actor, ok := h.recoveryActor(c)
	if !ok {
		return
	}
	if (c.Param("operation") == "plan" && c.Request.Method != http.MethodGet) ||
		(c.Param("operation") != "plan" && c.Request.Method != http.MethodPost) {
		c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "unsupported_normal_service_method"})
		return
	}
	switch c.Param("operation") {
	case "plan":
		plan, err := m.PlanNormalService(c.Request.Context(), actor)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "normal_service_not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"authorization": plan})
	case "activate":
		var authorization feishusynccontrol.NormalServiceAuthorization
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		if c.ShouldBindJSON(&authorization) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_normal_service_authorization"})
			return
		}
		if err := m.ActivateNormalService(c.Request.Context(), authorization, actor); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "normal_service_activation_rejected"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"normal_sync": "current_authorized_sources", "source_id_allowlist_required": false,
			"grant_id": authorization.ID,
		})
	case "deactivate":
		if err := m.DeactivateNormalService(c.Request.Context()); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "normal_service_deactivation_rejected"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"normal_sync": "revoked"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_normal_service_operation"})
	}
}
