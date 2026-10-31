package service

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

var exactPreflightLogs = struct {
	sync.Mutex
	last map[string]time.Time
}{last: make(map[string]time.Time)}

func logExactPersonalPreflightRefusal(
	ctx context.Context, owner *types.FeishuPersonalAttempt, stage string, permissions *[3]bool, err error,
) {
	fields := exactPersonalPreflightFields(owner, stage, permissions, err)
	key := owner.LogicalKnowledgeID + ":" + stage + ":" + fields["failure_class"].(string)
	exactPreflightLogs.Lock()
	now := time.Now()
	last := exactPreflightLogs.last[key]
	if now.Sub(last) < time.Minute {
		exactPreflightLogs.Unlock()
		return
	}
	if len(exactPreflightLogs.last) > 4096 {
		clear(exactPreflightLogs.last)
	}
	exactPreflightLogs.last[key] = now
	exactPreflightLogs.Unlock()
	logger.WarnWithFields(ctx, fields, "Personal exact preflight refused; pending protocol preserved")
}

// Only finite classifications and protocol IDs are logged. Never format the
// error, provider message, URL, token, file path, manifest or source content.
func exactPersonalPreflightFields(
	owner *types.FeishuPersonalAttempt, stage string, permissions *[3]bool, err error,
) logger.Fields {
	category := "source_proof_refused"
	switch {
	case errors.Is(err, ErrFeishuReconnect), errors.Is(err, ErrFeishuRefreshExpired):
		category = "oauth_reauthorization_required"
	case errors.Is(err, repository.ErrPersonalIdentity):
		category = "protocol_identity"
	case errors.Is(err, repository.ErrPersonalFence):
		category = "stale_generation"
	case errors.Is(err, gorm.ErrRecordNotFound):
		category = "authoritative_record_missing"
	case errors.Is(err, os.ErrPermission):
		category = "file_access_denied"
	case errors.Is(err, os.ErrNotExist):
		category = "file_not_found"
	case errors.Is(err, context.DeadlineExceeded):
		category = "deadline"
	case errors.Is(err, context.Canceled):
		category = "canceled"
	}
	fields := logger.Fields{
		"knowledge_id": owner.LogicalKnowledgeID, "attempt_id": owner.ID,
		"generation": owner.Fence, "preflight_stage": stage, "failure_class": category,
	}
	var api *core.APIError
	if errors.As(err, &api) {
		// Operation and Category can contain arbitrary strings in wrapped errors.
		// Keep the output vocabulary closed even for a malformed provider adapter.
		operation := "other"
		switch api.Operation {
		case "resolve_personal_resource", "check_personal_view_permission",
			"check_personal_export_permission", "check_personal_copy_permission":
			operation = api.Operation
		}
		fields["provider_operation"] = operation
		fields["provider_code"] = api.Code
		fields["provider_http_status"] = api.HTTPStatus
		fields["transport_class"] = core.TransportErrorCategory(api.Cause)
	}
	if permissions != nil {
		fields["view_allowed"] = permissions[0]
		fields["export_allowed"] = permissions[1]
		fields["copy_allowed"] = permissions[2]
	}
	return fields
}
