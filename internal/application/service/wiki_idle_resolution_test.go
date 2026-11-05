package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWikiIdleResolutionNeverSettlesBusinessOperations(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	db := f.DB
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}))
	repo := repository.NewWikiPageRepository(db)
	ctx := context.Background()
	require.NoError(t, repository.WriteWikiProcessingBlock(ctx, repo, 1, "public-two", "source_verification_required"))
	for _, kind := range []string{"wiki:ingest", "wiki:finalize"} {
		op := types.TaskPendingOp{
			TenantID: 1, TaskType: kind, Scope: "knowledge_base", ScopeID: "public-two",
			Op: "retained", DedupKey: "synthetic", Payload: []byte(`{"knowledge_id":"public-two-doc"}`),
		}
		require.NoError(t, db.Create(&op).Error)
		resolved, err := repository.ResolveWikiProcessingBlockIfIdle(ctx, repo, 1, "public-two")
		require.NoError(t, err)
		require.False(t, resolved)
		var retained types.TaskPendingOp
		require.NoError(t, db.First(&retained, op.ID).Error)
		require.Equal(t, op.Payload, retained.Payload)
		require.NoError(t, db.Model(&op).Update("scope", "synthetic_completed_archive").Error)
	}
	resolved, err := repository.ResolveWikiProcessingBlockIfIdle(ctx, repo, 1, "public-two")
	require.NoError(t, err)
	require.True(t, resolved)
	block, err := repository.ReadWikiProcessingBlock(ctx, repo, "public-two")
	require.NoError(t, err)
	require.Empty(t, block.Reason)
	var archived int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).
		Where("scope='synthetic_completed_archive'").Count(&archived).Error)
	require.EqualValues(t, 2, archived)
}
