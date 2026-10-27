package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// FeishuPersonalHandler defines feishu personal handler state.
type FeishuPersonalHandler struct {
	svc *service.FeishuPersonalService
}

// NewFeishuPersonalHandler constructs FeishuPersonalHandler.
func NewFeishuPersonalHandler(svc *service.FeishuPersonalService) *FeishuPersonalHandler {
	return &FeishuPersonalHandler{svc: svc}
}

func feishuFailureResponse(c *gin.Context, err error) {
	// Service errors from storage/ingestion can contain internal details. Stable
	// client errors carry no provider-controlled error bodies or credentials.
	if err != nil {
		if errors.Is(err, types.ErrPrivateKnowledgeBase) {
			c.JSON(http.StatusForbidden, gin.H{"error": "private_knowledge_base_forbidden"})
			return
		}
		if errors.Is(err, service.ErrFeishuSharingApproval) {
			c.JSON(http.StatusConflict, gin.H{
				"error":   "sharing_approval_required",
				"message": "个人文档发布到当前知识库需要明确的共享发布批准",
			})
			return
		}
		if errors.Is(err, types.ErrFeishuIdentityRequired) || errors.Is(err, types.ErrFeishuIdentityDisabled) {
			c.JSON(http.StatusConflict, gin.H{"error": "feishu_identity_unavailable", "message": "请选择明确且已启用的飞书身份"})
			return
		}
		// Preserve the service's live authorization decision. In particular, a
		// failed KB check is not a provider/export failure or duplicate verdict.
		switch {
		case errors.Is(err, access.ErrUnauthorized):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication_required"})
			return
		case errors.Is(err, access.ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": "feishu_kb_write_forbidden"})
			return
		case errors.Is(err, access.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "resource_not_found"})
			return
		}
		code, message := "feishu_operation_failed", "请检查飞书授权、文档权限和知识库配置后重试"
		switch {
		case errors.Is(err, core.ErrApplicationTableMedia):
			code, message = "feishu_table_media_not_supported", "表格包含尚未支持完整解析的图片或附件，请手动导"+
				"出核对后上传"
		case errors.Is(err, core.ErrApplicationTableIncomplete):
			code, message = "feishu_table_content_incomplete", "表格内容读取不完整或读取期间发生变化，请稍后重"+
				"试"
		case errors.Is(err, service.ErrFeishuApplicationRestricted):
			code, message = "feishu_application_test_only", "飞书文档导入尚未启用或当前账号无导入权限"
		case errors.Is(err, service.ErrFeishuIncompleteContent),
			errors.Is(err, core.ErrApplicationIncompleteContent):
			code, message = "feishu_incomplete_content", "该文档含尚未验证的内容或导出内容不完整，请手动"+
				"导出核对后上传；已有知识保留"
		case errors.Is(err, repository.ErrPersonalReviewRequired):
			code, message = "feishu_recovery_requires_review", "已有部分解析或索引结果，需要人工核查恢复；原知"+
				"识和文件已保留"
		case errors.Is(err, repository.ErrPersonalUpdateDisabled):
			code, message = "feishu_update_not_open", "文档更新暂未开放，已入库内容保持不变"
		case errors.Is(err, repository.ErrPersonalAlreadyImported):
			code, message = "feishu_already_imported", "该飞书资源已导入当前知识库，请查看已有知识条目"+
				"；不会使用其他用户的授权重新抓取"
		case errors.Is(err, core.ErrResponseTooLarge):
			code, message = "feishu_response_too_large", "飞书响应超过安全大小上限，请手动导出或拆分后上"+
				"传，已有知识保留"
		case errors.Is(err, service.ErrInvalidFileType):
			code, message = "feishu_unsupported_file_type", "该文件格式不在当前 KnowHub "+
				"解析器支持范围内，请先转换为可解析格式"
		case err.Error() == "system_aes_key must contain exactly 32 bytes":
			code, message = "feishu_encryption_key_required", "请先为测试服务配置 32 字节 SYSTEM_AES_KEY "+
				"后再保存应用"
		case errors.Is(err, core.ErrUnsupportedPersonalResource):
			code, message = "feishu_unsupported_resource", "该飞书资源暂不支持自动导入，请手动导出为 "+
				"PDF、Word、Excel 或 PPT 后上传"
		case errors.Is(err, service.ErrFeishuReconnect), errors.Is(err, core.ErrAuth):
			code, message = "feishu_reconnect_required", "飞书授权已失效，请重新连接账号"
		case errors.Is(err, core.ErrPermission), errors.Is(err, core.ErrNotFound):
			code, message = "feishu_permission_required", "当前飞书授权身份无权读取或导出该资源，已有入库"+
				"内容将保留"
		case errors.Is(err, core.ErrRateLimited):
			code, message = "feishu_rate_limited", "飞书限流，请稍后重试，已有入库内容将保留"
		case errors.Is(err, core.ErrTransient),
			errors.Is(err, context.DeadlineExceeded),
			strings.Contains(err.Error(), "export task timed out"):
			code, message = "feishu_temporary_failure", "飞书网络或导出暂时失败，请稍后重试，已有入库内"+
				"容将保留"
		case err.Error() == "feishu_export_missing_tables":
			code, message = "feishu_incomplete_export", "导出文件未包含全部数据表或工作表，请手动导出核"+
				"对后上传"
		case err.Error() == "feishu_sync_in_progress":
			code, message = "feishu_sync_in_progress", "此文档正在同步，请等待当前任务完成"
		case err.Error() == "identical_content_already_exists_in_knowledge_base":
			code, message = "feishu_duplicate_content", "当前知识库已存在相同内容"
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": code, "message": message})
	}
}

// Status provides the status operation.
func (h *FeishuPersonalHandler) Status(c *gin.Context) {
	c.JSON(200, h.svc.Status(c.Request.Context()))
}

// Config provides the config operation.
func (h *FeishuPersonalHandler) Config(c *gin.Context) {
	result, err := h.svc.AdminConfig(c.Request.Context())
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, result)
}

// SaveConfig provides the save config operation.
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

// TestConfig provides the test config operation.
func (h *FeishuPersonalHandler) TestConfig(c *gin.Context) {
	if err := h.svc.TestApp(c.Request.Context()); err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, gin.H{"valid": true})
}

// Authorize provides the authorize operation.
func (h *FeishuPersonalHandler) Authorize(c *gin.Context) {
	link, err := h.svc.Authorize(c.Request.Context())
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, gin.H{"authorization_url": link})
}

// Complete provides the complete operation.
func (h *FeishuPersonalHandler) Complete(c *gin.Context) {
	// These query fields are redacted by the existing request logger.
	if err := h.svc.Complete(c.Request.Context(), c.Query("state"), c.Query("code")); err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, gin.H{"connected": true})
}

// Disconnect provides the disconnect operation.
func (h *FeishuPersonalHandler) Disconnect(c *gin.Context) {
	if err := h.svc.Disconnect(c.Request.Context()); err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, gin.H{"connected": false})
}

// Import provides the import operation.
func (h *FeishuPersonalHandler) Import(c *gin.Context) {
	var input struct {
		URL      string `json:"url"`
		AuthMode string `json:"auth_mode"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": "invalid_url"})
		return
	}
	row, err := h.svc.ImportWithIdentity(c.Request.Context(), c.Param("id"), input.URL, input.AuthMode)
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, row)
}

// List returns the caller's import history for the authorized KB.
func (h *FeishuPersonalHandler) List(c *gin.Context) {
	rows, err := h.svc.ListImports(c.Request.Context(), c.Param("id"))
	if err != nil {
		feishuFailureResponse(c, err)
		return
	}
	c.JSON(200, rows)
}
