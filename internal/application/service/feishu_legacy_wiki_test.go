package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFeishuLegacyWikiWakeRetainsDurableWorkAfterTriggerFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}))
	ctx := context.Background()
	repo := repository.NewTaskPendingOpsRepository(db)
	kb := &types.KnowledgeBase{
		ID: "published-kb", TenantID: 7,
		IndexingStrategy: types.IndexingStrategy{WikiEnabled: true},
	}
	op := &types.TaskPendingOp{
		TenantID: 7, TaskType: types.TypeWikiIngest,
		Scope: types.TaskScopeKnowledgeBase, ScopeID: kb.ID, Op: "ingest", DedupKey: "published-doc",
	}
	require.NoError(t, repo.Enqueue(ctx, op))
	queue := &followUpEnqueuerRecorder{failFrom: 0, failErr: errors.New("Redis trigger temporarily unavailable")}
	svc := &knowledgeService{taskPendingRepo: repo, task: queue}
	require.Error(t, svc.wakeFeishuPublishedWiki(ctx, kb))
	queue.failFrom = -1
	require.NoError(t, svc.wakeFeishuPublishedWiki(ctx, kb))
	count, err := repo.PendingCount(ctx, types.TypeWikiIngest, types.TaskScopeKnowledgeBase, kb.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "waking a receipt retry must retain the original operation")
	queue.payloads(t, kb.ID)
	require.NoError(t, repo.DeleteByIDs(ctx, []int64{op.ID}))
	require.NoError(t, svc.wakeFeishuPublishedWiki(ctx, kb))
	require.Equal(t, 2, queue.count(), "a consumed lane must not keep generating triggers")
}
