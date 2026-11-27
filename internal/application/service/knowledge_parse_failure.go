package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/officeguard"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

// A durable outbox must not turn an exhausted/archived queue task into a fresh
// retry budget after restart. Still-processing rows retain normal recovery.
func documentDispatchFailureTerminal(ctx context.Context, current *types.Knowledge, err error) bool {
	if current == nil || current.ParseStatus != types.ParseStatusFailed {
		return false
	}
	if err == nil || errors.Is(err, asynq.SkipRetry) {
		return true
	}
	retries, available := asynq.GetRetryCount(ctx)
	limit, _ := asynq.GetMaxRetry(ctx)
	if n, retryLimit, ok := types.TaskRetryMetadataFromContext(ctx); ok {
		retries, limit, available = n, retryLimit, true
	}
	return !available || retries >= limit
}

// Only diagnosed permanent conditions stop early. Unknown parser errors keep
// the existing finite task retry budget; they never imply corrupt source data.
func classifyKnowledgeParseFailure(err error) (code, action string, permanent bool) {
	if officeguard.IsEncryptedError(err) {
		return "encrypted_file", "provide_unencrypted_copy", true
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "encryptedpackage"), strings.Contains(text, "file is encrypted"),
		strings.Contains(text, "password required"), strings.Contains(text, "文件已加密或受保护"):
		return "encrypted_file", "provide_unencrypted_copy", true
	case strings.Contains(text, "no valid workbook part"), strings.Contains(text, "not a zip file"):
		return "file_format_mismatch", "check_file_format", true
	case strings.Contains(text, "parser semantic chunk too large"):
		return "semantic_chunk_limit", "adjust_table_chunking", true
	case strings.Contains(text, "ssrf"), strings.Contains(text, "not allowed for security reasons"):
		return "source_address_blocked", "check_source_address", true
	case strings.Contains(text, "resource limit"), strings.Contains(text, "out of memory"):
		return "parser_resource_limit", "split_document", true
	}
	if isLikelyRateLimitError(err) {
		return "parser_rate_limited", "review_processing_trace", false
	}
	code, _ = docReaderFailure(err)
	return code, "review_processing_trace", false
}

func knowledgeParseFailureState(
	ctx context.Context, err error, last bool,
) (*types.KnowledgeParseFailure, string) {
	code, action, permanent := classifyKnowledgeParseFailure(err)
	retries, _ := asynq.GetRetryCount(ctx)
	limit, _ := asynq.GetMaxRetry(ctx)
	if n, retryLimit, ok := types.TaskRetryMetadataFromContext(ctx); ok {
		retries, limit = n, retryLimit
	}
	failure := &types.KnowledgeParseFailure{
		Code: code, State: "needs_attention", Action: action,
		Attempts: retries + 1, MaxAttempts: max(retries, limit) + 1,
		AutoRetry: !permanent && !last && retries < limit,
	}
	_, message := docReaderFailure(err)
	switch code {
	case "encrypted_file":
		message = "文件已加密或受保护，请提供未加密版本。"
		if officeguard.IsEncryptedError(err) {
			message = officeguard.RestrictionMessage(err)
		}
	case "file_format_mismatch":
		message = "文件内容与声明格式不符，请检查文件格式或重新导出。"
	case "semantic_chunk_limit":
		message = "表格单行内容超过解析限制，请调整表格分块设置。"
	case "source_address_blocked":
		message = "来源地址未通过安全检查，请检查来源地址配置。"
	case "parser_resource_limit":
		message = "文件超过当前解析资源限制，请拆分文件后重试。"
	case "parser_rate_limited":
		message = "解析服务请求受限，请稍后重试。"
	}
	if failure.AutoRetry {
		failure.State = "retry_scheduled"
		failure.Action = "wait_for_automatic_retry"
	}
	return failure, message
}

func (s *knowledgeService) recordKnowledgeParseFailure(
	ctx context.Context, knowledge *types.Knowledge, cause error, last bool,
) (*types.ReadResult, error) {
	if ctx.Err() != nil {
		return nil, cause
	}
	if aborted, _ := s.isKnowledgeAborted(ctx, knowledge.TenantID, knowledge.ID); aborted {
		return nil, nil
	}
	if s.isKnowledgeSourceReplaced(ctx, knowledge) {
		return nil, nil
	}
	failure, message := knowledgeParseFailureState(ctx, cause, last)
	if err := knowledge.SetParseFailure(failure); err != nil {
		return nil, err
	}
	knowledge.ErrorMessage = message
	knowledge.UpdatedAt = time.Now()
	if failure.AutoRetry {
		knowledge.ParseStatus = types.ParseStatusProcessing
	} else {
		knowledge.ParseStatus = types.ParseStatusFailed
	}
	if err := s.updateKnowledgeUnlessSourceReplaced(ctx, knowledge); err != nil {
		return nil, err
	}
	if _, _, permanent := classifyKnowledgeParseFailure(cause); permanent {
		return nil, errors.Join(asynq.SkipRetry, cause)
	}
	return nil, cause
}
