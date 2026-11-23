package service

import (
	"context"
	"slices"

	"github.com/Tencent/WeKnora/internal/application/repository"
)

func (h *streamSyncHandler) ConfirmReparsedDocument(
	ctx context.Context, root, externalID, version string,
) (bool, error) {
	if !safeLegacyIncrementalEnabled() || h.ds == nil || !legacyWikiDrive(h.ds.Type) ||
		h.config == nil || !slices.Contains(h.config.ResourceIDs, root) {
		return false, nil
	}
	if err := h.svc.checkSyncExecution(ctx); err != nil {
		return false, err
	}
	store, err := repository.FeishuLegacyStoreFor(h.svc.knowledgeService.GetRepository())
	if err != nil {
		return false, err
	}
	return store.ConfirmSourceReparsePublication(ctx, h.ds, root, externalID, version)
}
