package service

import (
	"context"
	"mime/multipart"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type sourceReparseMetadataRepo struct{ interfaces.KnowledgeRepository }

func (sourceReparseMetadataRepo) LoadPersonalReparseMetadata(
	_ context.Context, id string,
) (*repository.PersonalReparseMetadata, error) {
	if id != "doc" {
		return &repository.PersonalReparseMetadata{}, nil
	}
	return &repository.PersonalReparseMetadata{
		ID: "doc", TenantID: 7, KnowledgeBaseID: "kb", LegacySourceManaged: true, DataSourceID: "source",
	}, nil
}

func TestSourceReparseQueuedMixedBatchCannotSubmitOrdinaryPrefix(t *testing.T) {
	s := &knowledgeService{repo: sourceReparseMetadataRepo{}}
	_, _, err := s.reparseTaskScope(context.Background(), types.KnowledgeListReparsePayload{
		TenantID: 7, KnowledgeBaseID: "kb", KnowledgeIDs: []string{"ordinary", "doc"},
	})
	require.ErrorIs(t, err, asynq.SkipRetry,
		"a legacy queued batch must be refused before any body read or ordinary-prefix enqueue")
}

func TestSourceReparseRefusesBeforeOverridesCleanupAndEnqueue(t *testing.T) {
	s := &knowledgeService{
		repo: sourceReparseMetadataRepo{},
		kbService: &documentKBLookup{values: map[string]*types.KnowledgeBase{
			"kb": {ID: "kb", TenantID: 7},
		}},
	}
	ctx := types.WithCaller(context.Background(),
		types.Caller{TenantID: 7, UserID: "actor", Role: types.TenantRoleAdmin})
	ctx = context.WithValue(ctx, types.PrincipalContextKey, types.Principal{Type: types.PrincipalWebUser, ID: "actor"})
	// Every body loader, cleanup dependency and task enqueuer is deliberately
	// absent: reaching any of them would panic or destroy the retained version.
	_, err := s.ReparseKnowledge(ctx, "doc", &types.KnowledgeProcessOverrides{})
	var app *apperrors.AppError
	require.ErrorAs(t, err, &app)
	require.Equal(t, 409, app.HTTPCode)
	require.Equal(t, "feishu_source_sync_required", app.Details.(map[string]any)["reason"])
	require.Equal(t, false, app.Details.(map[string]any)["destructive_reparse_allowed"])
	_, err = s.ReplaceKnowledgeFile(ctx, "doc", &multipart.FileHeader{Filename: "replacement.docx"}, "", nil)
	require.ErrorAs(t, err, &app)
	require.Equal(t, 409, app.HTTPCode, "file replacement must refuse before uploading or swapping source columns")
	for _, caller := range []types.Caller{
		{TenantID: 8, UserID: "outsider", Role: types.TenantRoleAdmin},
		{TenantID: 7, UserID: "viewer", Role: types.TenantRoleViewer},
	} {
		denied := types.WithCaller(context.Background(), caller)
		denied = context.WithValue(denied, types.PrincipalContextKey,
			types.Principal{Type: types.PrincipalWebUser, ID: caller.UserID})
		require.ErrorAs(t, s.ValidateKnowledgeReparse(denied, "doc"), &app)
		require.Equal(t, 403, app.HTTPCode)
		require.Nil(t, app.Details, "authorization failures disclose no datasource identity")
	}
}
