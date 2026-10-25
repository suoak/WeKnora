package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// ListSharingApprovals returns the owner's bounded approval history.
func (h *FeishuPersonalHandler) ListSharingApprovals(c *gin.Context) {
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid_approval_history_offset"})
		return
	}
	rows, err := h.svc.ListSharingApprovals(c.Request.Context(), c.Param("id"), offset)
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, rows)
}

// RevokeSharingApproval withdraws the caller's own future publication approval.
func (h *FeishuPersonalHandler) RevokeSharingApproval(c *gin.Context) {
	if err := h.svc.RevokeSharingApproval(c.Request.Context(), c.Param("id"), c.Param("approvalID")); err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, gin.H{"revoked": true})
}
