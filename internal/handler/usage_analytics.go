package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type UsageAnalyticsHandler struct {
	service interfaces.UsageAnalyticsService
}

func NewUsageAnalyticsHandler(service interfaces.UsageAnalyticsService) *UsageAnalyticsHandler {
	return &UsageAnalyticsHandler{service: service}
}

// ReportInboundMCP accepts only safe invocation metadata. Caller identity and
// workspace attribution are derived by the service from authenticated context.
func (h *UsageAnalyticsHandler) ReportInboundMCP(c *gin.Context) {
	if _, ok := types.TenantAPIKeyScopeFromContext(c.Request.Context()); !ok {
		c.Error(errors.NewForbiddenError("MCP usage reports require API key authentication"))
		return
	}
	var report types.MCPUsageReport
	if err := c.ShouldBindJSON(&report); err != nil {
		c.Error(errors.NewBadRequestError("invalid MCP usage report"))
		return
	}
	report.SharedGateway = strings.EqualFold(strings.TrimSpace(c.GetHeader("X-WeKnora-MCP-Shared-Gateway")), "true")
	if err := h.service.RecordInboundMCP(c.Request.Context(), report); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true})
}

func parseUsageQuery(c *gin.Context) (types.UsageTimeRange, error) {
	q := types.UsageTimeRange{Interval: strings.ToLower(strings.TrimSpace(c.DefaultQuery("interval", "day"))), Sort: strings.ToLower(strings.TrimSpace(c.Query("sort")))}
	var err error
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		q.From, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return q, err
		}
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		q.To, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return q, err
		}
	}
	if !q.From.IsZero() && !q.To.IsZero() && !q.From.Before(q.To) {
		return q, errors.NewBadRequestError("from must be before to")
	}
	if raw := strings.TrimSpace(c.Query("tenant_id")); raw != "" {
		id, e := strconv.ParseUint(raw, 10, 64)
		if e != nil || id == 0 {
			return q, errors.NewBadRequestError("invalid tenant_id")
		}
		q.TenantID = &id
	}
	if raw := c.Query("page"); raw != "" {
		q.Page, _ = strconv.Atoi(raw)
	}
	if raw := c.Query("page_size"); raw != "" {
		q.PageSize, _ = strconv.Atoi(raw)
	}
	allowed := map[string]bool{"hour": true, "day": true, "week": true, "month": true}
	if !allowed[q.Interval] {
		return q, errors.NewBadRequestError("invalid interval")
	}
	return q, nil
}

func usageQuery(c *gin.Context, run func(types.UsageTimeRange) (any, error)) {
	q, err := parseUsageQuery(c)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	value, err := run(q)
	if err != nil {
		logger.Error(c.Request.Context(), err)
		c.Error(errors.NewInternalServerError("failed to query usage analytics"))
		return
	}
	c.JSON(http.StatusOK, value)
}

func (h *UsageAnalyticsHandler) Overview(c *gin.Context) {
	usageQuery(c, func(q types.UsageTimeRange) (any, error) { return h.service.Overview(c.Request.Context(), q) })
}
func (h *UsageAnalyticsHandler) Tenants(c *gin.Context) {
	usageQuery(c, func(q types.UsageTimeRange) (any, error) { return h.service.Tenants(c.Request.Context(), q) })
}
func (h *UsageAnalyticsHandler) TimeSeries(c *gin.Context) {
	usageQuery(c, func(q types.UsageTimeRange) (any, error) { return h.service.TimeSeries(c.Request.Context(), q) })
}
func (h *UsageAnalyticsHandler) Models(c *gin.Context) {
	usageQuery(c, func(q types.UsageTimeRange) (any, error) { return h.service.Models(c.Request.Context(), q) })
}
func (h *UsageAnalyticsHandler) MCP(c *gin.Context) {
	usageQuery(c, func(q types.UsageTimeRange) (any, error) { return h.service.MCP(c.Request.Context(), q) })
}
func (h *UsageAnalyticsHandler) KnowledgeBases(c *gin.Context) {
	usageQuery(c, func(q types.UsageTimeRange) (any, error) { return h.service.KnowledgeBases(c.Request.Context(), q) })
}
