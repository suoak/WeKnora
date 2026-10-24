package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
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
