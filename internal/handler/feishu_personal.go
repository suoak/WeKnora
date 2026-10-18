package handler

import (
	"context"
	"errors"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

type FeishuPersonalHandler struct {
	svc *service.FeishuPersonalService
}

func NewFeishuPersonalHandler(svc *service.FeishuPersonalService) *FeishuPersonalHandler {
	return &FeishuPersonalHandler{svc: svc}
}

func feishuFailureResponse(c *gin.Context, err error) {
	// Service errors from storage/ingestion can contain internal details. Stable
	// client errors carry no provider-controlled error bodies or credentials.
	if err != nil {
		code, message := "feishu_operation_failed", "请检查飞书授权、文档权限和知识库配置后重试"
		switch {
		case errors.Is(err, service.ErrInvalidFileType):
			code, message = "feishu_unsupported_file_type", "该文件格式不在当前 KnowHub 解析器支持范围内，请先转换为可解析格式"
		case err.Error() == "SYSTEM_AES_KEY must contain exactly 32 bytes":
			code, message = "feishu_encryption_key_required", "请先为测试服务配置 32 字节 SYSTEM_AES_KEY 后再保存应用"
		case errors.Is(err, core.ErrUnsupportedPersonalResource):
			code, message = "feishu_unsupported_resource", "该飞书资源暂不支持自动导入，请手动导出为 PDF、Word、Excel 或 PPT 后上传"
		case errors.Is(err, service.ErrFeishuReconnect), errors.Is(err, core.ErrAuth):
			code, message = "feishu_reconnect_required", "飞书授权已失效，请重新连接账号"
		case errors.Is(err, core.ErrPermission), errors.Is(err, core.ErrNotFound):
			code, message = "feishu_permission_required", "当前飞书账号无权读取或导出该资源，已有入库内容将保留"
		case errors.Is(err, core.ErrRateLimited):
			code, message = "feishu_rate_limited", "飞书限流，请稍后重试，已有入库内容将保留"
		case errors.Is(err, core.ErrTransient), errors.Is(err, context.DeadlineExceeded), strings.Contains(err.Error(), "export task timed out"):
			code, message = "feishu_temporary_failure", "飞书网络或导出暂时失败，请稍后重试，已有入库内容将保留"
		case err.Error() == "feishu_export_missing_tables":
			code, message = "feishu_incomplete_export", "导出文件未包含全部数据表或工作表，请手动导出核对后上传"
		case err.Error() == "feishu_sync_in_progress":
			code, message = "feishu_sync_in_progress", "此文档正在同步，请等待当前任务完成"
		case err.Error() == "identical_content_already_exists_in_knowledge_base":
			code, message = "feishu_duplicate_content", "当前知识库已存在相同内容"
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": code, "message": message})
	}
}

func (h *FeishuPersonalHandler) Status(c *gin.Context) {
	c.JSON(200, h.svc.Status(c.Request.Context()))
}

func (h *FeishuPersonalHandler) Browse(c *gin.Context) {
	result, err := h.svc.Browse(c.Request.Context(), c.Query("folder"), c.Query("page_token"))
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h *FeishuPersonalHandler) Config(c *gin.Context) {
	result, err := h.svc.AdminConfig(c.Request.Context())
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, result)
}
func (h *FeishuPersonalHandler) SaveConfig(c *gin.Context) {
	var cfg service.FeishuAppConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(400, gin.H{"error": "invalid_config"})
		return
	}
	if err := h.svc.SaveApp(c.Request.Context(), &cfg); err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, gin.H{"saved": true})
}
func (h *FeishuPersonalHandler) TestConfig(c *gin.Context) {
	if err := h.svc.TestApp(c.Request.Context()); err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, gin.H{"valid": true})
}
func (h *FeishuPersonalHandler) Authorize(c *gin.Context) {
	link, err := h.svc.Authorize(c.Request.Context())
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, gin.H{"authorization_url": link})
}
func (h *FeishuPersonalHandler) Complete(c *gin.Context) {
	// These query fields are redacted by the existing request logger.
	if err := h.svc.Complete(c.Request.Context(), c.Query("state"), c.Query("code")); err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, gin.H{"connected": true})
}
func (h *FeishuPersonalHandler) Disconnect(c *gin.Context) {
	if err := h.svc.Disconnect(c.Request.Context()); err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, gin.H{"connected": false})
}
func (h *FeishuPersonalHandler) Import(c *gin.Context) {
	var input struct {
		URL string `json:"url"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": "invalid_url"})
		return
	}
	row, err := h.svc.Import(c.Request.Context(), c.Param("id"), input.URL)
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, row)
}
func (h *FeishuPersonalHandler) List(c *gin.Context) {
	rows, err := h.svc.ListImports(c.Request.Context(), c.Param("id"))
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, rows)
}
