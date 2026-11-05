package types

import "strings"

// DataSourceSyncHealth describes the observed synchronization state without granting access.
type DataSourceSyncHealth struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Action  string `json:"action,omitempty"`
}

// DescribeSyncHealth exposes no source body or credentials. It explains the
// latest observed state; only the existing admission and publication gates grant access.
func (d *DataSource) DescribeSyncHealth() *DataSourceSyncHealth {
	if d.Type != "feishu" && d.Type != "feishu_drive" && d.Type != "lark" && d.Type != "lark_drive" {
		return nil
	}
	health := func(code, message, action string) *DataSourceSyncHealth {
		return &DataSourceSyncHealth{Code: code, Message: message, Action: action}
	}
	if d.Status == DataSourceStatusPaused {
		return health("paused", "已暂停自动同步", "resume")
	}
	if d.LegacyAccessState == "revoked" {
		return health("reauthorize", "来源授权已撤销，停止读取和更新", "reconnect")
	}
	errorText := strings.ToLower(d.ErrorMessage)
	if strings.Contains(errorText, "credential reference unavailable") ||
		strings.Contains(errorText, "authorization expired") ||
		strings.Contains(errorText, "requires reauthorization") {
		return health("reauthorize", "需要重新连接并验证飞书授权", "reconnect")
	}
	if strings.Contains(errorText, "feishu source permission denied") {
		return health("permission_denied", "来源权限不足，请检查飞书资源的查看、复制和导出权限", "check_permissions")
	}
	if d.LegacyAccessState == "hold" {
		return health("verification_required", "历史来源需要完整核验，已保留原数据与同步进度", "verify_source")
	}
	if d.LatestSyncLog != nil {
		if d.LatestSyncLog.Status == SyncLogStatusRunning {
			return health("retrying", "正在同步或自动重试", "view_progress")
		}
		if d.LatestSyncLog.Status == SyncLogStatusPartial {
			return health("partial", "部分文档同步失败，其他文档继续更新，失败内容未发布", "view_failed_documents")
		}
	}
	if d.Status == DataSourceStatusError {
		return health("retrying", "同步暂未完成，将按配置重新检查；可查看具体失败原因", "view_progress")
	}
	return health("healthy", "正常自动同步", "")
}
