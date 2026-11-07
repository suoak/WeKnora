package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

type dataSourceIngestError struct{ cause error }

func (e *dataSourceIngestError) Error() string { return "data source item not durably accepted" }
func (e *dataSourceIngestError) Unwrap() error { return e.cause }

// Retry a complete all-failed inventory only when every failure is known
// temporary. A sampled error list cannot establish that an unseen document
// was not rejected for authorization or integrity reasons.
func temporarySyncItemFailure(result *types.SyncResult) error {
	if result == nil || result.Total == 0 || result.Failed != result.Total ||
		len(result.Errors) != result.Failed {
		return nil
	}
	var causes []error
	for _, item := range result.Errors {
		switch item.Code {
		case "source_verification_unavailable":
			causes = append(causes, feishusafety.ErrSourceValidationUnavailable)
		case "embedding_budget_busy":
			causes = append(causes, feishusafety.ErrBudgetBusy)
		default:
			return nil
		}
	}
	return errors.Join(causes...)
}

func (s *DataSourceService) recordUnacceptedSync(
	ctx context.Context,
	ds *types.DataSource,
	log *types.SyncLog,
	result *types.SyncResult,
	cause error,
	wasPaused bool,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if errors.Is(cause, errSourceConsentPending) {
		return s.failSyncAttempt(ctx, ds, log, cause, "Feishu authorization update pending")
	}
	message := "Item admission failed; checkpoint retained"
	if errors.Is(cause, ErrFeishuSharingApproval) {
		// Keep the checkpoint before the rejected version. Approval must be
		// supplied by the owner; replaying exports cannot create permission.
		message = ErrFeishuSharingApproval.Error()
		cause = errors.Join(asynq.SkipRetry, cause)
		for i := range result.Errors {
			if result.Errors[i].Code == "ingest_failed" {
				result.Errors[i].Code = "sharing_approval_required"
				result.Errors[i].Message = "当前版本需要共享发布授权"
			}
		}
	}
	resultJSON, _ := result.ToJSON()
	if !syncTaskCanRetry(ctx, cause) {
		if err := s.updateSyncRunResult(
			ctx,
			ds,
			log,
			result,
			resultJSON,
			types.SyncLogStatusFailed,
			message,
			wasPaused,
		); err != nil {
			return err
		}
		return cause
	}
	log.ItemsTotal = result.Total
	log.ItemsCreated = result.Created
	log.ItemsUpdated = result.Updated
	log.ItemsDeleted = result.Deleted
	log.ItemsSkipped = result.Skipped
	log.ItemsFailed = result.Failed
	log.Result = resultJSON
	log.Status = types.SyncLogStatusRunning
	log.FinishedAt = nil
	log.ErrorMessage = "Item admission failed; retrying from accepted checkpoint"
	ds.LastSyncResult = resultJSON
	ds.ErrorMessage = log.ErrorMessage
	if err := s.writeSyncExecution(ctx, ds, log, true); err != nil {
		return err
	}
	return cause
}
