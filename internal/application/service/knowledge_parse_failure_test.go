package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeParseFailureBudgetAndManualActions(t *testing.T) {
	for _, tc := range []struct {
		err, code, action string
	}{
		{"file is encrypted: private-path", "encrypted_file", "provide_unencrypted_copy"},
		{"File contains no valid workbook part", "file_format_mismatch", "check_file_format"},
		{"parser semantic chunk too large: private-sheet", "semantic_chunk_limit", "adjust_table_chunking"},
		{"URL blocked by SSRF guard: secret-url", "source_address_blocked", "check_source_address"},
		{"resource limit: private-path", "parser_resource_limit", "split_document"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			ctx := types.WithTaskRetryMetadata(context.Background(), 0, 3)
			failure, message := knowledgeParseFailureState(ctx, errors.New(tc.err), false)
			require.Equal(t, tc.code, failure.Code)
			require.Equal(t, tc.action, failure.Action)
			require.Equal(t, "needs_attention", failure.State)
			require.False(t, failure.AutoRetry)
			require.NotContains(t, message, "private")
			require.NotContains(t, message, "secret")
		})
	}
	for retry := 0; retry <= 3; retry++ {
		ctx := types.WithTaskRetryMetadata(context.Background(), retry, 3)
		failure, message := knowledgeParseFailureState(ctx, context.DeadlineExceeded, retry == 3)
		require.Equal(t, retry < 3, failure.AutoRetry)
		require.Equal(t, retry+1, failure.Attempts)
		require.Equal(t, 4, failure.MaxAttempts)
		require.NotEmpty(t, message)
		if retry < 3 {
			require.Equal(t, "retry_scheduled", failure.State)
		} else {
			require.Equal(t, "needs_attention", failure.State)
		}
	}
	failure, _ := knowledgeParseFailureState(context.Background(), errors.New("unknown"), false)
	require.False(t, failure.AutoRetry, "missing queue metadata cannot promise an automatic retry")
	failure, message := knowledgeParseFailureState(types.WithTaskRetryMetadata(context.Background(), 0, 3),
		errors.New("429 too many requests: private-token"), false)
	require.Equal(t, "parser_rate_limited", failure.Code)
	require.True(t, failure.AutoRetry)
	require.NotContains(t, message, "private-token")
}

func TestKnowledgeParseFailureMetadataPreservesConfigurationAndClears(t *testing.T) {
	k := &types.Knowledge{Metadata: types.JSON(`{"process_overrides":{"graph_enabled":false},"external_id":"x"}`)}
	failure, _ := knowledgeParseFailureState(types.WithTaskRetryMetadata(context.Background(), 0, 3),
		context.DeadlineExceeded, false)
	require.NoError(t, k.SetParseFailure(failure))
	require.Contains(t, string(k.Metadata), "retry_scheduled")
	require.Contains(t, string(k.Metadata), "process_overrides")
	markKnowledgeProcessing(k, k.UpdatedAt)
	require.False(t, strings.Contains(string(k.Metadata), types.KnowledgeParseFailureKey))
	require.Contains(t, string(k.Metadata), "external_id")
	require.Contains(t, string(k.Metadata), "process_overrides")
}

type parseFailureRepository struct {
	interfaces.KnowledgeRepository
	current *types.Knowledge
	writes  int
}

func (r *parseFailureRepository) GetKnowledgeByID(context.Context, uint64, string) (*types.Knowledge, error) {
	snapshot := *r.current
	return &snapshot, nil
}

func (r *parseFailureRepository) UpdateKnowledge(_ context.Context, k *types.Knowledge) error {
	snapshot := *k
	r.current = &snapshot
	r.writes++
	return nil
}

func TestKnowledgeParseFailurePersistsBoundedRetryAndRespectsCancellation(t *testing.T) {
	ctx := types.WithTaskRetryMetadata(context.Background(), 0, 3)
	for _, tc := range []struct {
		name, status, path string
		cause              error
		writes             int
	}{
		{"temporary", types.ParseStatusProcessing, "original", context.DeadlineExceeded, 1},
		{"permanent", types.ParseStatusProcessing, "original", errors.New("file is encrypted"), 1},
		{"cancelled", types.ParseStatusCancelled, "original", context.DeadlineExceeded, 0},
		{"deleted", types.ParseStatusDeleting, "original", context.DeadlineExceeded, 0},
		{"replaced", types.ParseStatusProcessing, "replacement", context.DeadlineExceeded, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := &types.Knowledge{ID: "document", TenantID: 7, FilePath: "original"}
			repo := &parseFailureRepository{current: &types.Knowledge{
				ID: k.ID, TenantID: k.TenantID, FilePath: tc.path, ParseStatus: tc.status,
			}}
			s := &knowledgeService{repo: repo}
			_, err := s.recordKnowledgeParseFailure(ctx, k, tc.cause, false)
			require.Equal(t, tc.writes, repo.writes)
			if tc.writes == 0 {
				require.NoError(t, err)
				return
			}
			if tc.name == "permanent" {
				require.ErrorIs(t, err, asynq.SkipRetry)
				require.Equal(t, types.ParseStatusFailed, repo.current.ParseStatus)
			} else {
				require.ErrorIs(t, err, tc.cause)
				require.Equal(t, types.ParseStatusProcessing, repo.current.ParseStatus)
				require.Contains(t, string(repo.current.Metadata), "retry_scheduled")
			}
		})
	}
}
