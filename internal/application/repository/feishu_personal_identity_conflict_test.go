package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPersonalAuthModeConflictRollsBackWithoutMutation(t *testing.T) {
	conn, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
	require.NoError(t, err)
	repo := NewPersonalRecoveryRepository(db)
	repo.FirstCreateOnly = true
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .*feishu_personal_imports`).WillReturnRows(
		sqlmock.NewRows([]string{"id", "user_id", "auth_mode", "knowledge_id", "status"}).
			AddRow("old-import", "owner", types.FeishuApplicationGrant, "", "needs_attention"),
	)
	// No UPDATE, INSERT, publication, Claim or old-attempt mutation is allowed.
	mock.ExpectRollback()
	_, _, err = repo.Prepare(context.Background(), &types.FeishuPersonalImport{
		UserID: "owner", AppID: "app", KBID: "kb", ResourceType: "docx", DocumentToken: "doc",
		AuthMode: types.FeishuUserOAuth,
	}, &types.FeishuPersonalAttempt{AuthMode: types.FeishuUserOAuth})
	require.ErrorIs(t, err, ErrPersonalAuthModeConflict)
	require.ErrorIs(t, err, ErrPersonalIdentity)
	require.NoError(t, mock.ExpectationsWereMet())
}
