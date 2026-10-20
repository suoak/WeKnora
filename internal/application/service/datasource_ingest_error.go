package service

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
)

type dataSourceIngestError struct{ cause error }

func (e *dataSourceIngestError) Error() string { return "data source item not durably accepted" }
func (e *dataSourceIngestError) Unwrap() error { return e.cause }
func (s *DataSourceService) recordUnacceptedSync(ctx context.Context, ds *types.DataSource, log *types.SyncLog, result *types.SyncResult, cause error, wasPaused bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resultJSON, _ := result.ToJSON()
	if !syncTaskCanRetry(ctx, cause) {
		if err := s.updateSyncRunResult(ctx, ds, log, result, resultJSON, types.SyncLogStatusFailed, "Item admission failed; checkpoint retained", wasPaused); err != nil {
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
