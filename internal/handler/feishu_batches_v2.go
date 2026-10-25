package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *FeishuPersonalHandler) Preview(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
	var in struct {
		AuthMode     string   `json:"auth_mode"`
		ConnectionID string   `json:"connection_id"`
		Links        []string `json:"links"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "invalid_preview"})
		return
	}
	rows, err := h.svc.Preview(c.Request.Context(), c.Param("id"), in.AuthMode, in.ConnectionID, in.Links)
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, rows)
}
func (h *FeishuPersonalHandler) CreateBatch(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
	var in struct {
		AuthMode     string   `json:"auth_mode"`
		ConnectionID string   `json:"connection_id"`
		Links        []string `json:"links"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "invalid_batch"})
		return
	}
	row, err := h.svc.CreateBatch(c.Request.Context(), c.Param("id"), in.AuthMode, in.ConnectionID, c.GetHeader("Idempotency-Key"), in.Links)
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(202, row)
}
func (h *FeishuPersonalHandler) Batch(c *gin.Context) {
	row, err := h.svc.Batch(c.Request.Context(), c.Param("batchID"))
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, row)
}
func (h *FeishuPersonalHandler) RetryBatch(c *gin.Context) {
	if err := h.svc.RetryBatch(c.Request.Context(), c.Param("batchID")); err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(202, gin.H{"accepted": true})
}
func (h *FeishuPersonalHandler) ApproveSharing(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var in struct {
		ConnectionID string `json:"connection_id"`
		URL          string `json:"url"`
		Reason       string `json:"reason"`
		Confirmed    bool   `json:"confirmed"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "invalid_sharing_approval"})
		return
	}
	row, err := h.svc.ApproveSharing(c.Request.Context(), c.Param("id"), in.ConnectionID, in.URL, in.Reason, in.Confirmed)
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(201, row)
}
func (h *FeishuPersonalHandler) EnableSource(c *gin.Context) {
	var in struct {
		Schedule string `json:"schedule"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "invalid_source_schedule"})
		return
	}
	ds, err := h.svc.EnableImportSource(c.Request.Context(), c.Param("id"), c.Param("importID"), in.Schedule)
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(201, gin.H{"id": ds.ID, "status": ds.Status, "schedule": ds.SyncSchedule})
}
func (h *FeishuPersonalHandler) SourceAction(c *gin.Context) {
	if err := h.svc.ImportSourceAction(c.Request.Context(), c.Param("id"), c.Param("importID"), c.Param("action")); err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(202, gin.H{"accepted": true})
}

func (h *FeishuPersonalHandler) Collaboration(c *gin.Context) {
	rows, err := h.svc.Collaboration(c.Request.Context(), c.Param("batchID"))
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, rows)
}

func (h *FeishuPersonalHandler) Collaborate(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var in struct {
		Action  string `json:"action"`
		Contact string `json:"contact"`
		Note    string `json:"note"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "invalid_permission_collaboration"})
		return
	}
	row, err := h.svc.Collaborate(c.Request.Context(), c.Param("batchID"), c.Param("itemID"), in.Action, in.Contact, in.Note)
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(201, row)
}
