package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

func (s *DataSourceService) restrictFailedFeishuItem(
	ctx context.Context, ds *types.DataSource, item *types.FetchedItem,
) error {
	if !safeLegacyIncrementalEnabled() || !legacyWikiDrive(ds.Type) {
		return nil
	}
	// Only an explicit per-document permission/not-found refusal can be local.
	// Authentication, incomplete listings and unknown identities remain closed.
	if feishuDocumentFetchFailureCode(ds, item) == "feishu_document_access_unavailable" {
		store, err := repository.FeishuLegacyStoreFor(s.knowledgeService.GetRepository())
		if err != nil {
			return err
		}
		empty, err := store.UnpublishedResource(ctx, ds, item.ExternalID)
		if err != nil || empty {
			return err
		}
		return store.RestrictDocument(context.WithoutCancel(ctx), ds, item.ExternalID)
	}
	return s.legacySourceUncertain(ctx, ds, true)
}

func (s *DataSourceService) retainVerifiedFeishuItem(
	ctx context.Context, ds *types.DataSource, item *types.FetchedItem,
) error {
	// Only signed full reconciliation needs a current traversal receipt for
	// unchanged public snapshots. Ordinary failed candidates remain unpublished.
	if !types.LegacyReconciliationRead(ctx, ds.ID) {
		return nil
	}
	store, err := repository.FeishuLegacyStoreFor(s.knowledgeService.GetRepository())
	if err != nil {
		return err
	}
	receipt, err := store.RetainVerifiedVersion(ctx, ds, item)
	if err != nil {
		// Unpublished candidates have no public version to retain. Inventory
		// validation still rejects any public row lacking a verified receipt.
		logger.Warnf(ctx, "feishu retained version not verified source_id=%s; final inventory remains authoritative", ds.ID)
		return nil
	}
	types.AddLegacyReceipt(ctx, *receipt)
	return nil
}
