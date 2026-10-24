package service

import (
	"errors"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
)

func feishuDocumentFailureCode(ds *types.DataSource, item *types.FetchedItem, err error) string {
	if !safeLegacyIncrementalEnabled() || !legacyWikiDrive(ds.Type) || item.IsDeleted || item.ExternalID == "" {
		return ""
	}
	// A known exhausted allocation is backpressure, never an unknown guard state.
	// Its request is still denied; only the document remains pending for retry.
	if errors.Is(err, feishusafety.ErrBudgetBusy) {
		return "embedding_budget_busy"
	}
	var fence *repository.LegacyFenceError
	if errors.As(err, &fence) && fence.Reason == "source_revision_content_conflict" {
		return fence.Reason
	}
	var refusal *feishuStagingRefusal
	if errors.As(err, &refusal) {
		switch refusal.reason {
		case "versioned_image_staging_required", "audio_not_supported",
			"empty_parsed_content", "chunk_count_out_of_bounds":
			return "safe_staging_" + refusal.reason
		}
	}
	return ""
}
