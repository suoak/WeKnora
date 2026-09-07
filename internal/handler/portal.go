package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type PortalHandler struct{ service interfaces.PortalService }

func NewPortalHandler(service interfaces.PortalService) *PortalHandler {
	return &PortalHandler{service: service}
}

func portalActor(c *gin.Context) (string, uint64) {
	uid, _ := types.UserIDFromContext(c.Request.Context())
	tid, _ := types.TenantIDFromContext(c.Request.Context())
	return uid, tid
}

func portalTenantID(c *gin.Context) (uint64, bool) {
	raw := c.Param("tenant_id")
	if raw == "" {
		raw = c.Param("id")
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		_ = c.Error(apperrors.NewValidationError("workspace id must be a positive integer"))
		c.Abort()
		return 0, false
	}
	return id, true
}

func portalError(c *gin.Context, err error) {
	var appErr *apperrors.AppError
	switch {
	case errors.As(err, &appErr):
		_ = c.Error(appErr)
	case errors.Is(err, service.ErrPortalNotFound), errors.Is(err, service.ErrPortalRequestNotFound):
		_ = c.Error(apperrors.NewNotFoundError(err.Error()))
	case errors.Is(err, service.ErrPortalReviewForbidden), errors.Is(err, service.ErrPortalInteractionForbidden):
		_ = c.Error(apperrors.NewForbiddenError(err.Error()))
	case errors.Is(err, service.ErrPortalPendingExists), errors.Is(err, service.ErrPortalAlreadyMember),
		errors.Is(err, service.ErrPortalMembershipSuspended), errors.Is(err, service.ErrPortalRequestNotPending),
		errors.Is(err, service.ErrPortalNotRequestable), errors.Is(err, service.ErrPortalApplicantUnavailable):
		_ = c.Error(apperrors.NewConflictError(err.Error()))
	case errors.Is(err, service.ErrPortalInvalidReason), errors.Is(err, service.ErrPortalInvalidConfig),
		errors.Is(err, service.ErrPortalInvalidCategory), errors.Is(err, service.ErrPortalInvalidStage):
		_ = c.Error(apperrors.NewValidationError(err.Error()))
	default:
		_ = c.Error(apperrors.NewInternalServerError("portal operation failed"))
	}
	c.Abort()
}

func (h *PortalHandler) ListStages(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.service.ListStages()})
}

func (h *PortalHandler) ListSpaces(c *gin.Context) {
	uid, activeTenantID := portalActor(c)
	rows, err := h.service.ListSpaces(c.Request.Context(), uid, activeTenantID, interfaces.PortalListQuery{
		Query: c.Query("q"), Stage: c.Query("stage"), Category: c.Query("category"),
	})
	if err != nil {
		portalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows, "total": len(rows)})
}

func (h *PortalHandler) ListMySpaces(c *gin.Context) {
	uid, _ := portalActor(c)
	rows, err := h.service.ListMySpaces(c.Request.Context(), uid)
	if err != nil {
		portalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows, "total": len(rows)})
}

type portalAccessRequestBody struct {
	Reason string `json:"reason" binding:"required"`
}

const portalRequestBodyLimit = 8 << 10

func bindStrictPortalJSON(c *gin.Context, dst any) error {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, portalRequestBodyLimit+1))
	if err != nil || len(body) > portalRequestBodyLimit {
		return service.ErrPortalInvalidConfig
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return service.ErrPortalInvalidConfig
	}
	return nil
}

func (h *PortalHandler) CreateAccessRequest(c *gin.Context) {
	tenantID, ok := portalTenantID(c)
	if !ok {
		return
	}
	var body portalAccessRequestBody
	if err := bindStrictPortalJSON(c, &body); err != nil || strings.TrimSpace(body.Reason) == "" {
		portalError(c, service.ErrPortalInvalidReason)
		return
	}
	uid, _ := portalActor(c)
	request, err := h.service.CreateAccessRequest(c.Request.Context(), uid, tenantID, body.Reason)
	if err != nil {
		portalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": request})
}

func (h *PortalHandler) ListMyAccessRequests(c *gin.Context) {
	uid, _ := portalActor(c)
	rows, err := h.service.ListMyAccessRequests(c.Request.Context(), uid)
	if err != nil {
		portalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows, "total": len(rows)})
}

func (h *PortalHandler) CancelAccessRequest(c *gin.Context) {
	uid, _ := portalActor(c)
	if err := h.service.CancelAccessRequest(c.Request.Context(), uid, c.Param("request_id")); err != nil {
		portalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *PortalHandler) ListTenantAccessRequests(c *gin.Context) {
	tenantID, ok := portalTenantID(c)
	if !ok {
		return
	}
	uid, _ := portalActor(c)
	rows, err := h.service.ListTenantAccessRequests(c.Request.Context(), uid, tenantID)
	if err != nil {
		portalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows, "total": len(rows)})
}

type portalReviewBody struct {
	ReviewNote string `json:"review_note"`
}

func (h *PortalHandler) review(c *gin.Context, approve bool) {
	tenantID, ok := portalTenantID(c)
	if !ok {
		return
	}
	var body portalReviewBody
	if c.Request.ContentLength > 0 {
		if err := bindStrictPortalJSON(c, &body); err != nil {
			portalError(c, service.ErrPortalInvalidConfig)
			return
		}
	}
	uid, _ := portalActor(c)
	var err error
	if approve {
		err = h.service.ApproveAccessRequest(c.Request.Context(), uid, tenantID, c.Param("request_id"), body.ReviewNote)
	} else {
		err = h.service.RejectAccessRequest(c.Request.Context(), uid, tenantID, c.Param("request_id"), body.ReviewNote)
	}
	if err != nil {
		portalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *PortalHandler) ApproveAccessRequest(c *gin.Context) { h.review(c, true) }
func (h *PortalHandler) RejectAccessRequest(c *gin.Context)  { h.review(c, false) }

func (h *PortalHandler) ResolveInteraction(c *gin.Context) {
	tenantID, ok := portalTenantID(c)
	if !ok {
		return
	}
	uid, activeTenantID := portalActor(c)
	path, err := h.service.ResolveInteraction(c.Request.Context(), uid, activeTenantID, tenantID)
	if err != nil {
		portalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"action": "navigate", "path": path}})
}

func (h *PortalHandler) ListAdminSpaces(c *gin.Context) {
	rows, err := h.service.ListAdminSpaces(c.Request.Context())
	if err != nil {
		portalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows, "total": len(rows)})
}

func (h *PortalHandler) GetAdminSpace(c *gin.Context) {
	tenantID, ok := portalTenantID(c)
	if !ok {
		return
	}
	item, err := h.service.GetAdminSpace(c.Request.Context(), tenantID)
	if err != nil {
		portalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

func (h *PortalHandler) UpdateConfig(c *gin.Context) {
	tenantID, ok := portalTenantID(c)
	if !ok {
		return
	}
	var body types.PortalConfigUpdateRequest
	if err := bindStrictPortalJSON(c, &body); err != nil {
		portalError(c, service.ErrPortalInvalidConfig)
		return
	}
	uid, _ := portalActor(c)
	item, err := h.service.UpdateConfig(c.Request.Context(), uid, tenantID, &body)
	if err != nil {
		portalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

func (h *PortalHandler) setStatus(c *gin.Context, status types.PortalStatus) {
	tenantID, ok := portalTenantID(c)
	if !ok {
		return
	}
	uid, _ := portalActor(c)
	item, err := h.service.SetStatus(c.Request.Context(), uid, tenantID, status)
	if err != nil {
		portalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

func (h *PortalHandler) Publish(c *gin.Context) {
	h.setStatus(c, types.PortalStatusPublished)
}

func (h *PortalHandler) Unpublish(c *gin.Context) {
	h.setStatus(c, types.PortalStatusDraft)
}

func (h *PortalHandler) Archive(c *gin.Context) {
	h.setStatus(c, types.PortalStatusArchived)
}
