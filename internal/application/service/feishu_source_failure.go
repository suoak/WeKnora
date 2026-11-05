package service

import (
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
)

func feishuSourceBindingFailureMessage(err error) string {
	switch {
	case errors.Is(err, ErrFeishuReconnect), errors.Is(err, core.ErrAuth):
		return "Feishu connection requires reauthorization"
	case errors.Is(err, ErrFeishuSharingApproval):
		return "Feishu source publication consent requires verification"
	case errors.Is(err, ErrFeishuSourceRebindRequired):
		return "Feishu source identity requires verification"
	case errors.Is(err, access.ErrForbidden), errors.Is(err, core.ErrPermission),
		errors.Is(err, core.ErrPersonalReadDenied), errors.Is(err, core.ErrPersonalCopyDenied),
		errors.Is(err, core.ErrPersonalCopyExportDenied), errors.Is(err, core.ErrPersonalCopyOnlyReview):
		return "Feishu source permission denied"
	default:
		return "Feishu source binding requires verification"
	}
}
