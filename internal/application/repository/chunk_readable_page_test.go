package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestReadableTextPageLargeDocumentAndWorkerFences(t *testing.T) {
	db := setupChunkTestDB(t)
	require.NoError(t, db.Exec("CREATE TABLE knowledges(id text,knowledge_base_id text)").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledges VALUES ('doc','kb')").Error)
	for i := range 1007 {
		row := makeChunk("kb", "doc", types.ChunkTypeText)
		row.ChunkIndex = i
		require.NoError(t, db.Create(row).Error)
	}
	for _, row := range []*types.Chunk{
		makeChunk("kb", "doc", types.ChunkTypeParentText),
		makeChunk("other", "doc", types.ChunkTypeText),
	} {
		require.NoError(t, db.Create(row).Error)
	}
	disabled := makeChunk("kb", "doc", types.ChunkTypeText)
	disabled.IsEnabled = false
	require.NoError(t, db.Create(disabled).Error)
	require.NoError(t, db.Model(disabled).Update("is_enabled", false).Error)
	deleted := makeChunk("kb", "doc", types.ChunkTypeText)
	require.NoError(t, db.Create(deleted).Error)
	require.NoError(t, db.Delete(deleted).Error)
	foreign := makeChunk("kb", "doc", types.ChunkTypeText)
	foreign.TenantID = 2
	require.NoError(t, db.Create(foreign).Error)
	repo := NewChunkRepository(types.EnableLegacyReadGuards(db)).(*chunkRepository)
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
	seen := map[string]bool{}
	for offset := 0; offset < 1007; offset += 100 {
		rows, total, err := repo.ListReadableTextPage(ctx, 1, "doc", "kb", offset, 100)
		require.NoError(t, err)
		require.EqualValues(t, 1007, total)
		require.Len(t, rows, min(100, 1007-offset))
		for i, row := range rows {
			require.False(t, seen[row.ID])
			seen[row.ID] = true
			require.Equal(t, offset+i, row.ChunkIndex)
		}
	}
	require.Len(t, seen, 1007)
	rows, total, err := repo.ListReadableTextPage(ctx, 1, "doc", "kb", 37, 13)
	require.NoError(t, err)
	require.EqualValues(t, 1007, total)
	require.Len(t, rows, 13)
	require.Equal(t, 37, rows[0].ChunkIndex)
	rows, total, err = repo.ListReadableTextPage(ctx, 1, "doc", "kb", 2000, 100)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.EqualValues(t, 1007, total)
	for _, target := range []struct {
		tenant uint64
		kb     string
		limit  int
	}{{2, "kb", 100}, {1, "other", 100}, {1, "kb", 101}, {1, "kb", 0}} {
		rows, _, err := repo.ListReadableTextPage(ctx, target.tenant, "doc", target.kb, 0, target.limit)
		require.ErrorIs(t, err, types.ErrExactDocumentScope)
		require.Nil(t, rows)
	}
	refuseAt = calls + 2
	rows, total, err = repo.ListReadableTextPage(ctx, 1, "doc", "kb", 0, 100)
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	require.Nil(t, rows, "revoked output never escapes the repository")
	require.Zero(t, total)
}

func TestPortalDocumentIdentityKeepsMissingAndPrivateDenialsNeutral(t *testing.T) {
	db := setupChunkTestDB(t)
	require.NoError(t, db.Exec("CREATE TABLE knowledges(id text,knowledge_base_id text,deleted_at datetime)").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledges VALUES ('doc','kb',NULL)").Error)
	store := NewEnterpriseAccessRepository(db)
	kb, err := store.LookupPortalDocumentKB(context.Background(), "doc")
	require.NoError(t, err)
	require.Equal(t, "kb", kb)
	_, err = store.LookupPortalDocumentKB(context.Background(), "missing")
	require.ErrorIs(t, err, access.ErrForbidden)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("deny_private_identity", func(tx *gorm.DB) {
		_ = tx.AddError(types.ErrPrivateKnowledgeBase)
	}))
	_, err = store.LookupPortalDocumentKB(context.Background(), "doc")
	require.ErrorIs(t, err, access.ErrForbidden)
}
