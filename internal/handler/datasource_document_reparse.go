package handler

import (
	"errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ReparseSourceDocument accepts one controlled source-document request.
func (h *DataSourceHandler) ReparseSourceDocument(c *gin.Context) {
	if _, err := uuid.Parse(c.Param("knowledge_id")); err != nil {
		_ = c.Error(apperrors.NewBadRequestError("Invalid document ID"))
		return
	}
	_, ownerStatus, message := h.getOwnedDataSource(c.Request.Context(), h.getTenantID(c), c.Param("id"))
	if ownerStatus != http.StatusOK {
		c.JSON(ownerStatus, gin.H{"error": message})
		return
	}
	service, ok := h.service.(interfaces.DataSourceDocumentReparseService)
	if !ok {
		_ = c.Error(apperrors.NewServiceUnavailableError("Controlled document reparsing is unavailable"))
		return
	}
	var request struct {
		ProcessConfig *types.KnowledgeProcessOverrides `json:"process_config"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			_ = c.Error(apperrors.NewBadRequestError("Invalid document reparse configuration"))
			return
		}
	}
	status, err := service.RequestDocumentReparse(c.Request.Context(), c.Param("id"), c.Param("knowledge_id"),
		request.ProcessConfig)
	if err != nil {
		writeDocumentReparseError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": status})
}

// SourceDocumentReparseStatus returns the selected document's latest request state.
func (h *DataSourceHandler) SourceDocumentReparseStatus(c *gin.Context) {
	if _, err := uuid.Parse(c.Param("knowledge_id")); err != nil {
		_ = c.Error(apperrors.NewBadRequestError("Invalid document ID"))
		return
	}
	_, ownerStatus, message := h.getOwnedDataSource(c.Request.Context(), h.getTenantID(c), c.Param("id"))
	if ownerStatus != http.StatusOK {
		c.JSON(ownerStatus, gin.H{"error": message})
		return
	}
	service, ok := h.service.(interfaces.DataSourceDocumentReparseService)
	if !ok {
		_ = c.Error(apperrors.NewServiceUnavailableError("Controlled document reparsing is unavailable"))
		return
	}
	status, err := service.DocumentReparseStatus(c.Request.Context(), c.Param("id"), c.Param("knowledge_id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": nil})
		return
	}
	if err != nil {
		writeDocumentReparseError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": status})
}

func writeDocumentReparseError(c *gin.Context, err error) {
	if app, ok := apperrors.IsAppError(err); ok {
		_ = c.Error(app)
		return
	}
	var fence *repository.LegacyFenceError
	if errors.As(err, &fence) || errors.Is(err, repository.ErrLegacyVersionBusy) {
		_ = c.Error(apperrors.NewConflictError("Document source changed or a reparse request is already in progress"))
		return
	}
	_ = c.Error(apperrors.NewServiceUnavailableError("Document reparsing is temporarily unavailable"))
}
