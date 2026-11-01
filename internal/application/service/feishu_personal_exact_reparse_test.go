package service

import (
	"context"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalReparsePreservesProtocolAndOutputs(t *testing.T) {
	f, owner, _ := pendingExactFixture(t)
	ctx := context.WithValue(f.ctx, types.PrincipalContextKey,
		types.Principal{Type: types.PrincipalWebUser, ID: "A"})
	before := exactPendingSnapshot(t, f, owner.ID)
	_, err := f.ks.ReparseKnowledge(ctx, owner.LogicalKnowledgeID, nil)
	var app *apperrors.AppError
	require.ErrorAs(t, err, &app)
	require.Equal(t, 409, app.HTTPCode)
	require.Contains(t, app.Message, "已发布内容保留")
	require.JSONEq(t, string(before), string(exactPendingSnapshot(t, f, owner.ID)))
	require.NoError(t, f.ks.ValidateKnowledgeReparse(ctx, "ordinary-upload-without-personal-protocol"))

	denied := types.WithCaller(context.Background(),
		types.Caller{TenantID: 2, UserID: "outsider", Role: types.TenantRoleViewer})
	denied = context.WithValue(denied, types.PrincipalContextKey,
		types.Principal{Type: types.PrincipalWebUser, ID: "outsider"})
	require.ErrorAs(t, f.ks.ValidateKnowledgeReparse(denied, owner.LogicalKnowledgeID), &app)
	require.Equal(t, 403, app.HTTPCode)
	require.JSONEq(t, string(before), string(exactPendingSnapshot(t, f, owner.ID)))

	f.ks.personalRecovery = nil
	require.ErrorAs(t, f.ks.ValidateKnowledgeReparse(ctx, owner.LogicalKnowledgeID), &app)
	require.Equal(t, 503, app.HTTPCode, "disabling the recovery runner cannot enable destructive reparse")
	require.NoError(t, f.ks.ValidateKnowledgeReparse(ctx, "ordinary-upload-without-personal-protocol"))
}

func TestPersonalReparseEmbeddedDependencyPreflightIsReadOnly(t *testing.T) {
	f, owner, intent := pendingExactFixture(t)
	t.Setenv("FEISHU_PERSONAL_EXACT_EMBEDDED_XLSX", "1")
	before := exactPendingSnapshot(t, f, owner.ID)
	// This fixture's proved file is a PDF, not a closed DOCX/XLSX package.
	// An unsupported dependency cannot acquire a claim just because the
	// source/file authorization itself is valid.
	ready, err := f.p.firstCreateDispatchable(context.Background(), intent)
	require.Error(t, err)
	require.False(t, ready)
	require.JSONEq(t, string(before), string(exactPendingSnapshot(t, f, owner.ID)))
}
