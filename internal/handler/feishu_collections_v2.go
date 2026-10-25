package handler

import (
	"net/http"
	"strconv"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/gin-gonic/gin"
)

// CreateCollection selects one explicit directory for durable discovery.
func (h *FeishuPersonalHandler) CreateCollection(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var in struct {
		AuthMode     string `json:"auth_mode"`
		ConnectionID string `json:"connection_id"`
		Kind         string `json:"kind"`
		Parent       string `json:"parent_id"`
		Space        string `json:"space_id"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "invalid_collection"})
		return
	}
	row, err := h.svc.CreateCollection(c.Request.Context(), c.Param("id"), service.FeishuResourceQuery{
		AuthMode: in.AuthMode, ConnectionID: in.ConnectionID, Kind: in.Kind, Parent: in.Parent, Space: in.Space,
	})
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(202, row)
}

// ListCollections returns the owner's discovery controls and partial failures.
func (h *FeishuPersonalHandler) ListCollections(c *gin.Context) {
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid_collection_history_offset"})
		return
	}
	rows, err := h.svc.ListCollections(c.Request.Context(), c.Param("id"), offset)
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, rows)
}

// CollectionAction manages discovery without resuming held document sources.
func (h *FeishuPersonalHandler) CollectionAction(c *gin.Context) {
	if err := h.svc.CollectionAction(c.Request.Context(), c.Param("id"), c.Param("collectionID"),
		c.Param("action")); err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(202, gin.H{"accepted": true})
}

// CollectionPages returns the owner's retained page batches and directory paths.
func (h *FeishuPersonalHandler) CollectionPages(c *gin.Context) {
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid_collection_history_offset"})
		return
	}
	rows, err := h.svc.CollectionPages(c.Request.Context(), c.Param("id"), c.Param("collectionID"), offset)
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, rows)
}
