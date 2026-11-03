package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestExactDocumentBatchWriteFence(t *testing.T) {
	for _, mode := range []string{"large batch", "expired at commit", "mixed identity", "superseded foreign identity"} {
		t.Run(mode, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
			require.NoError(t, err)
			checks := 0
			expired := errors.New("expired owner")
			ctx, err := types.WithExactDocumentScope(context.Background(), types.ExactDocumentScope{
				TenantID: 1, KnowledgeID: "doc", KBID: "kb", Read: func(context.Context) error { return nil },
				Commit: func(*gorm.DB) error {
					checks++
					if mode == "expired at commit" && checks == 2 {
						return expired
					}
					return nil
				},
			})
			require.NoError(t, err)
			var infos []*types.IndexInfo
			rows := sqlmock.NewRows([]string{"id"})
			for i := range 101 {
				infos = append(infos, &types.IndexInfo{
					SourceID: fmt.Sprint(i), KnowledgeID: "doc", KnowledgeBaseID: "kb",
				})
				rows.AddRow(i + 1)
			}
			mixed := mode == "mixed identity" || mode == "superseded foreign identity"
			if mixed {
				infos[0].KnowledgeID = "other"
				if mode == "superseded foreign identity" {
					infos = append(infos, &types.IndexInfo{SourceID: "0", KnowledgeID: "doc", KnowledgeBaseID: "kb"})
				}
			} else {
				mock.ExpectBegin()
				mock.ExpectQuery(`INSERT INTO "embeddings"`).WillReturnRows(rows)
				if mode == "expired at commit" {
					mock.ExpectRollback()
				} else {
					mock.ExpectCommit()
				}
			}
			err = (&pgRepository{db: db}).BatchSave(ctx, infos, nil)
			if mixed {
				require.ErrorIs(t, err, types.ErrExactDocumentScope)
				require.Zero(t, checks, "foreign rows must be rejected before taking protocol locks")
			} else {
				if mode == "expired at commit" {
					require.ErrorIs(t, err, expired)
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, 2, checks,
					"retain pre/post checks without holding the protocol locks for per-row repeats")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
