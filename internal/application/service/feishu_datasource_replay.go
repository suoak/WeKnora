package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Changed versions must not fall through to the legacy delete/create path.
// This safety error stays active until a validated staging writer is wired.
var (
	ErrFeishuStagingRequired = errors.New(
		"feishu safe staging writer is not available for changed source version",
	)
	ErrFeishuDispatchUnconfirmed = errors.New("feishu durable dispatch is not confirmed")
)

func isFeishuAdmissionSource(kind string) bool {
	switch kind {
	case "feishu", "feishu_drive", "lark", "lark_drive", "feishu_personal":
		return true
	}
	return false
}

func reconcileFeishuReplay(
	ctx context.Context,
	service interfaces.KnowledgeService,
	existing *types.Knowledge,
	item *types.FetchedItem,
) error {
	if len(item.Content) == 0 {
		return ErrFeishuStagingRequired
	}
	digest := md5.Sum(item.Content)
	if existing.FileHash != hex.EncodeToString(digest[:]) || existing.FileName != item.FileName {
		return ErrFeishuStagingRequired
	}
	// ReparseKnowledge destroys chunks before dispatch and is not a safe
	// recovery primitive for an uncertain publication. Keep the existing ID
	// and refuse acknowledgement until the durable outbox can resume it.
	if existing.ParseStatus == types.ParseStatusPending || existing.ParseStatus == types.ParseStatusFailed {
		recovery, ok := service.(interfaces.KnowledgeDispatchRecovery)
		if !ok {
			return ErrFeishuDispatchUnconfirmed
		}
		if err := recovery.ResumeKnowledgeDispatch(ctx, existing); err != nil {
			return err
		}
		return types.NewDuplicateFileError(existing)
	}

	switch existing.ParseStatus {
	case types.ParseStatusProcessing, types.ParseStatusFinalizing, types.ParseStatusCompleted:
		return types.NewDuplicateFileError(existing)
	default:
		return ErrFeishuDispatchUnconfirmed
	}
}
