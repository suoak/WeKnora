package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWikiBlockedLanesWakeWithoutStalledDocuments(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := wikiBlockedLaneDB(t, dialect)
			queue := &dispatchQueue{}
			h := NewHousekeepingService(db, nil, nil, queue)
			ctx := context.Background()
			type pendingRow struct {
				ID      int64
				Payload string
			}
			var before []pendingRow
			require.NoError(t, db.Table("task_pending_ops").Select("id,payload").Order("id").Scan(&before).Error)
			h.rearmExpiredWikiBlocks(ctx)
			tasks := queue.snapshot()
			require.Len(t, tasks, 2, "expired blocks wake both lanes even with no pending Knowledge rows")
			seen := map[string]bool{}
			for _, task := range tasks {
				var payload WikiIngestPayload
				require.NoError(t, json.Unmarshal(task.Payload(), &payload))
				require.EqualValues(t, 1, payload.TenantID)
				require.Equal(t, "eligible", payload.KnowledgeBaseID)
				require.Equal(t, "zh-CN", payload.Language)
				seen[task.Type()] = true
			}
			require.True(t, seen[types.TypeWikiIngest])
			require.True(t, seen[types.TypeWikiFinalize])
			h.rearmExpiredWikiBlocks(ctx)
			require.Len(t, queue.snapshot(), 2, "each lane has a bounded local wake-up cadence")
			var after []pendingRow
			require.NoError(t, db.Table("task_pending_ops").Select("id,payload").Order("id").Scan(&after).Error)
			require.Equal(t, before, after, "no operation, block or payload is rewritten by a wake-up")
		})
	}
}

func TestWikiBlockedLaneQueueFailureDoesNotLoseWakeup(t *testing.T) {
	db := wikiBlockedLaneDB(t, "sqlite")
	queue := &dispatchQueue{down: true}
	h := NewHousekeepingService(db, nil, nil, queue)
	h.rearmExpiredWikiBlocks(context.Background())
	require.Empty(t, queue.snapshot())
	queue.setDown(false)
	h.rearmExpiredWikiBlocks(context.Background())
	require.Len(t, queue.snapshot(), 2)
}

func TestWikiBlockedLaneReplicaWakeupsCoalesce(t *testing.T) {
	db := wikiBlockedLaneDB(t, "sqlite")
	redis := miniredis.RunT(t)
	queue := asynq.NewClient(asynq.RedisClientOpt{Addr: redis.Addr()})
	t.Cleanup(func() { require.NoError(t, queue.Close()) })
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: redis.Addr()})
	t.Cleanup(func() { require.NoError(t, inspector.Close()) })
	for range 2 {
		h := NewHousekeepingService(db, nil, nil, queue)
		h.rearmExpiredWikiBlocks(context.Background())
	}
	info, err := inspector.GetQueueInfo(types.QueueWiki)
	require.NoError(t, err)
	require.Equal(t, 2, info.Pending, "replicas enqueue one trigger per durable lane")
}

func wikiBlockedLaneDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	db := syncdb.Open(t, dialect)
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}))
	require.NoError(t, db.Exec("CREATE TABLE knowledge_bases(id text PRIMARY KEY,tenant_id bigint,deleted_at timestamp)").Error)
	require.NoError(t, db.Exec("CREATE TABLE tenants(id bigint PRIMARY KEY,status text,deleted_at timestamp)").Error)
	require.NoError(t, db.Exec("INSERT INTO tenants(id,status) VALUES(1,'active'),(2,'active')").Error)
	require.NoError(t, db.Exec("UPDATE tenants SET deleted_at=CURRENT_TIMESTAMP WHERE id=2").Error)
	now := time.Now()
	for _, name := range []string{"eligible", "cooling", "claimed", "deleted", "wrong-tenant", "deleted-tenant", "malformed"} {
		tenant := uint64(1)
		if name == "deleted-tenant" {
			tenant = 2
		}
		require.NoError(t, db.Exec("INSERT INTO knowledge_bases(id,tenant_id) VALUES(?,?)", name, tenant).Error)
		if name == "deleted" {
			require.NoError(t, db.Exec("UPDATE knowledge_bases SET deleted_at=CURRENT_TIMESTAMP WHERE id=?", name).Error)
		}
		block := repository.WikiProcessingBlock{Reason: "source_verification_required",
			CheckedAt: now.Add(-time.Hour), RetryAfter: now.Add(-time.Minute)}
		if name == "cooling" {
			block.RetryAfter = now.Add(time.Hour)
		}
		payload, err := json.Marshal(block)
		require.NoError(t, err)
		if name == "malformed" {
			payload = []byte(`{}`)
		}
		require.NoError(t, db.Create(&types.TaskPendingOp{TenantID: tenant,
			Scope: "wiki_processing_state", ScopeID: name, TaskType: "wiki:state", Op: "blocked",
			DedupKey: name, Payload: payload}).Error)
		if name == "wrong-tenant" {
			tenant = 2
		}
		for _, taskType := range []string{types.TypeWikiIngest, types.TypeWikiFinalize} {
			op := types.TaskPendingOp{TenantID: tenant, Scope: types.TaskScopeKnowledgeBase,
				ScopeID: name, TaskType: taskType, Op: "ingest", DedupKey: name + taskType,
				Payload: json.RawMessage(`{"op":"ingest","knowledge_id":"doc","language":"zh-CN"}`)}
			if name == "claimed" {
				op.ClaimedAt = &now
			}
			require.NoError(t, db.Create(&op).Error)
		}
	}
	return db
}
