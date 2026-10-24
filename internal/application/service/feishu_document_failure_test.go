package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuDocumentFailureClassification(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	ds := &types.DataSource{Type: "feishu_drive"}
	item := &types.FetchedItem{ExternalID: "doc"}
	for _, tc := range []struct {
		err  error
		code string
	}{
		{&repository.LegacyFenceError{Reason: "source_revision_content_conflict"}, "source_revision_content_conflict"},
		{
			&feishuStagingRefusal{reason: "versioned_image_staging_required"},
			"safe_staging_versioned_image_staging_required",
		},
		{&repository.LegacyFenceError{Reason: "source_revision_regressed"}, ""},
		{&repository.LegacyFenceError{Reason: "cross_version_parent"}, ""},
		{&feishuStagingRefusal{reason: "parser_result_unavailable"}, ""},
		{feishusafety.ErrBudgetBusy, "embedding_budget_busy"},
		{feishusafety.ErrClosed, ""},
		{types.ErrStaleSyncExecution, ""},
		{ErrFeishuStagingRequired, ""},
		{context.Canceled, ""},
		{errors.New("unknown"), ""},
	} {
		require.Equal(t, tc.code, feishuDocumentFailureCode(ds, item, tc.err))
	}
	item.IsDeleted = true
	require.Empty(t, feishuDocumentFailureCode(ds, item,
		&repository.LegacyFenceError{Reason: "source_revision_content_conflict"}))
}

func TestFeishuDocumentFailureWithUnchangedDocumentsIsPartial(t *testing.T) {
	result := &types.SyncResult{Total: 1, Failed: 1, Unchanged: 2}
	require.NoError(t, allFetchedItemsFailedError(result))
	status, _ := streamingSyncOutcome(result)
	require.Equal(t, types.SyncLogStatusPartial, status)
	result.Unchanged = 0
	require.Error(t, allFetchedItemsFailedError(result))
}

func TestFeishuDocumentFailureParserRowLimit(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	ds := &types.DataSource{Type: "feishu"}
	item := &types.FetchedItem{ExternalID: "oversized-row", SourceResourceID: "root"}
	const detail = "sheet='synthetic', row=40, size=7888, hard_max_chars=7500)"
	for _, prefix := range []string{
		"parser semantic chunk too large; set xlsx_context_column_count to enable cell-level splitting (",
		"parser semantic chunk too large; xlsx_context_column_count must leave at least one payload column (",
	} {
		for _, kind := range []string{"xls", "xlsx", "xlsm"} {
			err := feishuDocumentParserRefusal(&types.ReadResult{Error: prefix + detail}, kind)
			require.Error(t, err)
			require.Equal(t, "safe_staging_parser_semantic_row_too_large", feishuDocumentFailureCode(ds, item, err))
		}
	}
	for _, result := range []*types.ReadResult{
		nil,
		{Error: "backend unavailable"},
		{Error: "PermissionError"},
		{Error: "parser semantic chunk too large; unrecognized error"},
		{Error: "parser semantic chunk too large; set xlsx_context_column_count to enable cell-level splitting ("},
	} {
		err := feishuDocumentParserRefusal(result, "xls")
		require.Error(t, err)
		require.Empty(t, feishuDocumentFailureCode(ds, item, err), "unknown parser faults still abort")
	}
	require.NoError(t, feishuDocumentParserRefusal(&types.ReadResult{MarkdownContent: "complete content"}, "xls"))
	require.Empty(t, feishuDocumentFailureCode(ds, item, feishuDocumentParserRefusal(
		&types.ReadResult{Error: "parser semantic chunk too large; set " +
			"xlsx_context_column_count to enable cell-level splitting (" + detail}, "docx")))
}

func TestFeishuDocumentFetchFailureClassification(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	ds := &types.DataSource{Type: "feishu_drive"}
	item := &types.FetchedItem{ExternalID: "doc", SourceResourceID: "root", Metadata: map[string]string{
		"failure_stage": "fetch_document", "feishu_error_category": "transient",
	}}
	require.Equal(t, "feishu_document_fetch_transient", feishuDocumentFetchFailureCode(ds, item))
	item.Metadata["feishu_error_category"] = "rate_limit"
	require.Equal(t, "feishu_document_fetch_rate_limited", feishuDocumentFetchFailureCode(ds, item))
	for _, category := range []string{"auth", "permission", "not_found", "unclassified", "permanent"} {
		item.Metadata["feishu_error_category"] = category
		require.Empty(t, feishuDocumentFetchFailureCode(ds, item))
	}
	item.Metadata["feishu_error_category"] = "transient"
	item.Metadata["failure_stage"] = "list_children"
	require.Empty(t, feishuDocumentFetchFailureCode(ds, item), "partial listings stay fatal")
	item.Metadata["failure_stage"] = "fetch_document"
	item.IsDeleted = true
	require.Empty(t, feishuDocumentFetchFailureCode(ds, item))
}

func TestFeishuDocumentFailureUnlistedIsNotSuccess(t *testing.T) {
	h := &streamSyncHandler{result: &types.SyncResult{Unchanged: 1}}
	h.ObserveDocument("root", "unlisted-doc", "pending_unlisted")
	h.ObserveDocument("overlapping-root", "unlisted-doc", "pending_unlisted")
	require.Equal(t, 1, h.result.Failed)
	require.Equal(t, "feishu_document_pending_unlisted", h.result.Errors[0].Code)
	status, _ := streamingSyncOutcome(h.result)
	require.Equal(t, types.SyncLogStatusPartial, status)
}
