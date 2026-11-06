package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestExactWorkerTextChunksUseBoundedLivePages(t *testing.T) {
	db := setupChunkTestDB(t)
	require.NoError(t, db.Exec("CREATE TABLE knowledges(id text,knowledge_base_id text)").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledges VALUES ('doc','kb'),('foreign','other')").Error)
	var ids []string
	for i := range 205 {
		row := makeChunk("kb", "doc", types.ChunkTypeText)
		row.ChunkIndex = i
		require.NoError(t, db.Create(row).Error)
		ids = append(ids, row.ID)
	}
	foreign := makeChunk("other", "foreign", types.ChunkTypeText)
	require.NoError(t, db.Create(foreign).Error)
	repo := NewChunkRepository(types.EnableLegacyReadGuards(db))
	ctx := types.WithExecutionTenant(context.Background(), 1)
	calls, refuseAt := 0, 0
	ctx, err := types.WithExactDocumentScope(ctx, types.ExactDocumentScope{
		TenantID: 1, KnowledgeID: "doc", KBID: "kb", Read: func(context.Context) error {
			calls++
			if calls == refuseAt {
				return types.ErrExactDocumentScope
			}
			return nil
		}, Commit: func(*gorm.DB) error { return nil },
	})
	require.NoError(t, err)
	rows, err := repo.ListChunksByKnowledgeID(ctx, 1, "doc")
	require.NoError(t, err)
	require.Len(t, rows, 205)
	require.Equal(t, 7, calls, "one issue, two live source checks per 100-row page")
	for i, row := range rows {
		require.Equal(t, i, row.ChunkIndex)
		require.Equal(t, "doc", row.KnowledgeID)
	}
	for _, target := range []struct {
		tenant uint64
		doc    string
	}{{2, "doc"}, {1, "foreign"}} {
		rows, err = repo.ListChunksByKnowledgeID(ctx, target.tenant, target.doc)
		require.ErrorIs(t, err, types.ErrExactDocumentScope)
		require.Nil(t, rows)
	}
	refuseAt = calls + 4
	rows, err = repo.ListChunksByKnowledgeID(ctx, 1, "doc")
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	require.Nil(t, rows, "revocation on a later page discards all accumulated rows")
	refuseAt = 0
	before := calls
	rows, err = repo.ListChunksByID(ctx, 1, ids)
	require.NoError(t, err)
	require.Len(t, rows, len(ids))
	require.Equal(t, before+6, calls, "model input revalidation must also avoid per-row remote checks")
	rows, err = repo.ListChunksByID(ctx, 1, []string{foreign.ID})
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	require.Nil(t, rows)
	rows, err = repo.ListChunksByID(ctx, 1, []string{ids[0], "missing"})
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	require.Nil(t, rows)
	rows, err = repo.ListChunksByID(ctx, 2, ids)
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	require.Nil(t, rows)
	rows, err = repo.ListChunksByID(ctx, 1, []string{ids[0], ids[0]})
	require.NoError(t, err)
	require.Len(t, rows, 1, "preserve existing duplicate-ID query semantics")
	refuseAt = calls + 4
	rows, err = repo.ListChunksByID(ctx, 1, ids)
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	require.Nil(t, rows)
	refuseAt = 0
	row, err := repo.GetChunkByID(ctx, 1, ids[0])
	require.NoError(t, err)
	require.Equal(t, "doc", row.KnowledgeID)
	row, err = repo.GetChunkByID(ctx, 1, foreign.ID)
	require.ErrorIs(t, err, ErrChunkNotFound, "exclude unrelated bodies in SQL, before any row proof")
	require.Nil(t, row)
	row, err = repo.GetChunkByID(ctx, 2, ids[0])
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	require.Nil(t, row)
}
