package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

func documentPolicyResponse(source *types.DataSource) (gin.H, error) {
	policy, err := source.ParseDocumentDeferrals()
	if err != nil {
		return nil, err
	}
	if policy == nil {
		policy = []types.SyncDocumentDeferral{}
	}
	return gin.H{"data": gin.H{"documents": policy, "policy_hash": types.DocumentDeferralHash(policy)}}, nil
}

// GetDocumentDeferrals returns a source's audited policy and compare-and-swap hash.
func (h *DataSourceHandler) GetDocumentDeferrals(c *gin.Context) {
	if h.getTenantID(c) == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	source, status, msg := h.getOwnedDataSource(c.Request.Context(), h.getTenantID(c), c.Param("id"))
	if status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}
	// Policy audit contains document identities; do not inherit the metadata-only
	// exception used to inspect recovery controls for a source on HOLD.
	if _, err := h.kbService.GetKnowledgeBaseByID(c.Request.Context(), source.KnowledgeBaseID); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "document diagnostics unavailable"})
		return
	}
	response, err := documentPolicyResponse(source)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "document policy unconfirmed"})
		return
	}
	c.JSON(http.StatusOK, response)
}

// PutDocumentDeferrals replaces an eligible source policy using its current hash.
func (h *DataSourceHandler) PutDocumentDeferrals(c *gin.Context) {
	if h.getTenantID(c) == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	_, status, msg := h.getOwnedDataSource(c.Request.Context(), h.getTenantID(c), c.Param("id"))
	if status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}
	var request struct {
		ExpectedPolicyHash string                       `json:"expected_policy_hash" binding:"required"`
		Documents          []types.SyncDocumentDeferral `json:"documents"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected policy hash and exact document identities required"})
		return
	}
	service, ok := h.service.(interfaces.DataSourceDocumentDeferralService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "document policy service unavailable"})
		return
	}
	source,
		err := service.ReplaceDocumentDeferrals(c.Request.Context(),
		c.Param("id"),
		request.ExpectedPolicyHash,
		request.Documents)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	response, err := documentPolicyResponse(source)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "document policy unconfirmed"})
		return
	}
	c.JSON(http.StatusOK, response)
}
