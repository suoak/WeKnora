package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/officeguard"
	"github.com/Tencent/WeKnora/internal/types"
)

// A verified content refusal is not evidence of missing source access or a
// source deletion. Full-reconciliation receipts still require the existing
// authority checks, so this does not restore a held or revoked old version.
func (s *DataSourceService) rejectEncryptedSyncItem(ctx context.Context, ds *types.DataSource,
	item *types.FetchedItem, result *types.SyncResult, cause error,
) error {
	result.Failed++
	recordSyncError(
		result,
		types.SyncItemError{Title: item.Title, Code: "office_encrypted", Message: officeguard.RestrictionMessage(cause)},
	)
	if local := feishuDocumentFailureCode(ds, item, cause); local != "" {
		if err := s.retainVerifiedFeishuItem(ctx, ds, item); err != nil {
			return &dataSourceIngestError{cause: err}
		}
		if err := s.checkSyncExecution(ctx); err != nil {
			return err
		}
		return &datasource.DocumentFailureError{Code: local, Cause: cause}
	}
	return nil
}
