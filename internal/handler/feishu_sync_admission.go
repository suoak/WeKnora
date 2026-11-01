package handler

import (
	"errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/gin-gonic/gin"
)

func writeSyncAdmissionFailure(c *gin.Context, err error) bool {
	var rejected *datasource.SyncAdmissionError
	code, message, status := "", "", http.StatusConflict
	if errors.As(err, &rejected) {
		code = rejected.Reason
		switch code {
		case "sync_source_paused":
			message = "数据源已暂停"
		case "sync_source_protected":
			message = "数据源暂不可同步，请联系管理员"
		case "sync_source_permission_denied":
			message = "当前账号无权同步此来源"
			status = http.StatusForbidden
		case "sync_policy_expired":
			message = "飞书同步策略已到期，请联系管理员恢复"
		case "sync_source_not_approved":
			message = "该来源尚未启用同步，请联系管理员"
		case "sync_source_config_changed":
			message = "来源配置已变化，请联系管理员重新核验"
		case "sync_source_authority_changed":
			message = "来源授权已变化，请联系管理员核验"
			status = http.StatusForbidden
		case "sync_recovery_review_pending":
			message = "飞书同步服务正在恢复，请联系管理员"
		case "sync_platform_stopped", "sync_safety_unavailable":
			message = "飞书同步服务暂时停止，管理员正在恢复"
			status = http.StatusServiceUnavailable
		default:
			code = "sync_safety_unavailable"
			message = "飞书同步服务暂时停止，管理员正在恢复"
			status = http.StatusServiceUnavailable
		}
	} else if errors.Is(err, core.ErrAuth) {
		code, message, status = "sync_feishu_authorization_expired", "飞书授权失效，请重新授权", http.StatusForbidden
	} else if errors.Is(err, datasource.ErrSyncAlreadyRunning) {
		code, message = "sync_already_running", "同步中，请查看实际同步进度"
	} else {
		return false
	}
	c.JSON(status, gin.H{"error": message, "code": code})
	return true
}
