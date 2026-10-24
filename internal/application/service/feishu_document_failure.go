package service

import (
	"errors"
	"strings"

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
			"empty_parsed_content", "chunk_count_out_of_bounds", "parser_semantic_row_too_large":
			return "safe_staging_" + refusal.reason
		}
	}
	return ""
}

func feishuDocumentParserRefusal(parsed *types.ReadResult, fileType string) error {
	if parsed == nil {
		return &feishuStagingRefusal{reason: "parser_result_unavailable"}
	}
	if parsed.Error == "" {
		return nil
	}
	// These exact Excel parser refusals are deterministic document limits.
	// Preserve the configured limit and context columns, never publish a
	// truncated row, and keep unknown parser/backend failures fatal.
	if fileType == "xls" || fileType == "xlsx" || fileType == "xlsm" {
		for _, prefix := range []string{
			"parser semantic chunk too large; set xlsx_context_column_count to enable cell-level splitting (",
			"parser semantic chunk too large; xlsx_context_column_count must leave at least one payload column (",
		} {
			if strings.HasPrefix(parsed.Error, prefix) && strings.Contains(parsed.Error, "hard_max_chars=") &&
				strings.HasSuffix(parsed.Error, ")") {
				return &feishuStagingRefusal{reason: "parser_semantic_row_too_large"}
			}
		}
	}
	return &feishuStagingRefusal{reason: "parser_result_unavailable"}
}

func feishuDocumentFetchFailureCode(ds *types.DataSource, item *types.FetchedItem) string {
	if !safeLegacyIncrementalEnabled() || !legacyWikiDrive(ds.Type) || item.IsDeleted ||
		item.ExternalID == "" || item.SourceResourceID == "" || item.Metadata["failure_stage"] != "fetch_document" {
		return ""
	}
	switch item.Metadata["feishu_error_category"] {
	case "transient":
		return "feishu_document_fetch_transient"
	case "rate_limit":
		return "feishu_document_fetch_rate_limited"
	default:
		return ""
	}
}
