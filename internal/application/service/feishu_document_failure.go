package service

import (
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishudocx"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/types"
)

func feishuDocumentFailureCode(ds *types.DataSource, item *types.FetchedItem, err error) string {
	if !safeLegacyIncrementalEnabled() || !legacyWikiDrive(ds.Type) || item.IsDeleted || item.ExternalID == "" {
		return ""
	}
	if errors.Is(err, ErrInvalidFileType) {
		return "unsupported_file_type"
	}
	if errors.Is(err, feishusafety.ErrProviderFailure) {
		if errors.Is(err, feishusafety.ErrClosed) || errors.Is(err, types.ErrStaleSyncExecution) {
			return ""
		}
		var response *feishusafety.ProviderResponseError
		if errors.As(err, &response) && response.Status == 403 {
			return "embedding_provider_http_403"
		}
		return "embedding_provider_failed"
	}
	// A known exhausted allocation is backpressure, never an unknown guard state.
	// Its request is still denied; only the document remains pending for retry.
	if errors.Is(err, feishusafety.ErrBudgetBusy) {
		return "embedding_budget_busy"
	}
	if errors.Is(err, feishusafety.ErrSourceValidationUnavailable) {
		return "source_verification_unavailable"
	}
	if errors.Is(err, types.ErrFeishuStagingCapacity) {
		return "safe_staging_capacity_insufficient"
	}
	// Package validation is a deterministic document refusal. Keep the failed
	// candidate unpublished and allow traversal to verify unrelated documents.
	// The final transaction still requires a receipt for every public document.
	if errors.Is(err, feishudocx.ErrInvalidXLSXPackage) {
		return "safe_staging_invalid_xlsx_package"
	}
	if errors.Is(err, anydoc.ErrResourceLimit) {
		return "safe_staging_parser_resource_limit"
	}
	var changed *feishuSnapshotChangedError
	if item.CurrentSnapshot != nil && errors.As(err, &changed) && sameSnapshotSourceBinding(item, changed.current) {
		return "source_snapshot_changed"
	}
	var fence *repository.LegacyFenceError
	if errors.As(err, &fence) {
		switch fence.Reason {
		case "source_revision_content_conflict":
			return fence.Reason
		}
	}
	var refusal *feishuStagingRefusal
	if errors.As(err, &refusal) {
		switch refusal.reason {
		case "versioned_image_staging_required", "audio_not_supported",
			"empty_parsed_content", "chunk_count_out_of_bounds", "parser_semantic_row_too_large",
			"image_enrichment_failed", "image_enrichment_timeout", "image_assets_unavailable",
			"embedded_workbook_unsupported",
			"encrypted_file", "file_format_mismatch", "source_address_blocked", "parser_resource_limit",
			"parser_timeout", "parser_busy":
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
	// Only the internal reader's explicit empty-Document result is local.
	// Missing results, unknown backend errors and contradictory nonempty
	// payloads remain fatal; error message text alone cannot advance a cursor.
	if parsed.Metadata["parser_failure_protocol"] == "v1" &&
		parsed.Metadata["parser_failure_code"] == "empty_parsed_content" &&
		parsed.MarkdownContent == "" && len(parsed.ImageRefs) == 0 &&
		len(parsed.ParsedChunks) == 0 && len(parsed.ParsedSegments) == 0 && len(parsed.SourceBlocks) == 0 {
		return &feishuStagingRefusal{reason: "empty_parsed_content"}
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
	if code, _, permanent := classifyKnowledgeParseFailure(errors.New(parsed.Error)); permanent {
		return &feishuStagingRefusal{reason: code}
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
	case "permission", "not_found":
		return "feishu_document_access_unavailable"
	default:
		return ""
	}
}
