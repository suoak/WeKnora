package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// This is a scheduling experiment, not a model-throughput or publication test.
// The unchanged claim/settle implementation and generation lease are exercised
// against PostgreSQL, with controlled map latency and at most two map workers.
// No claim or map result alone is allowed to release a document's counter.
func TestWikiSmallBatchClaimSettlePostgres(t *testing.T) {
	for _, size := range []int{1, 2} {
		t.Run(fmt.Sprintf("batch%d-map%d", size, size), func(t *testing.T) {
			db := syncdb.Open(t, "postgres")
			require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}))
			require.NoError(t, db.Exec("CREATE TABLE knowledge_bases (id text PRIMARY KEY, tenant_id bigint, deleted_at timestamptz)").Error)
			require.NoError(t, db.Exec("CREATE TABLE knowledges (id text PRIMARY KEY, pending_subtasks_count integer, parse_status text)").Error)
			kb := "batch-test-" + uuid.NewString()
			require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id,tenant_id) VALUES (?,1)", kb).Error)
			ctx := types.WithExecutionTenant(context.Background(), 1)
			repo := repository.NewTaskPendingOpsRepository(db)
			svc := &wikiIngestService{pendingRepo: repo}
			for i := 0; i < 4; i++ {
				id := fmt.Sprintf("doc-%d", i)
				payload, err := json.Marshal(WikiPendingOp{KnowledgeID: id, Op: WikiOpIngest, Attempt: 7})
				require.NoError(t, err)
				require.NoError(t, repo.Enqueue(ctx, &types.TaskPendingOp{TenantID: 1,
					TaskType: wikiTaskType, Scope: wikiTaskScope, ScopeID: kb, DedupKey: id,
					Op: WikiOpIngest, Payload: payload}))
				require.NoError(t, db.Exec("INSERT INTO knowledges VALUES (?,1,'finalizing')", id).Error)
			}
			other := &types.TaskPendingOp{TenantID: 1, TaskType: wikiTaskType, Scope: wikiTaskScope,
				ScopeID: "other-kb", DedupKey: "other-doc", Op: WikiOpIngest, Payload: []byte(`{}`)}
			require.NoError(t, repo.Enqueue(ctx, other))
			addr := os.Getenv("WIKI_BATCH_TEST_REDIS_ADDR")
			if addr == "" {
				addr = miniredis.RunT(t).Addr()
				t.Log("Redis backend: miniredis; set WIKI_BATCH_TEST_REDIS_ADDR for real Redis qualification")
			} else {
				t.Log("Redis backend: explicitly configured isolated Redis")
			}
			client := redis.NewClient(&redis.Options{Addr: addr})
			t.Cleanup(func() { _ = client.Close() })
			require.NoError(t, client.Ping(ctx).Err())
			started := time.Now()
			var active, peak atomic.Int32
			batches := 0
			err := withWorkerWikiGenerationLease(ctx, client, kb, func(locked context.Context) error {
				for {
					ops, ids, claims, err := svc.claimPendingListWithOwnership(locked, kb, size)
					if err != nil {
						return err
					}
					if len(ids) == 0 {
						return nil
					}
					require.Len(t, ops, size)
					require.Len(t, claims, size)
					owner := types.WithWikiClaimOwner(locked, 1, kb, claims)
					// A second worker cannot enter the same generation, even
					// though the SQL batch size and map limit were increased.
					require.ErrorIs(t, withWorkerWikiGenerationLease(ctx, client, kb, func(context.Context) error {
						t.Error("concurrent generation entered protected work")
						return nil
					}), ErrWikiIngestConcurrent)
					group, mapCtx := errgroup.WithContext(owner)
					group.SetLimit(size)
					for range ops {
						group.Go(func() error {
							n := active.Add(1)
							defer active.Add(-1)
							for previous := peak.Load(); n > previous; previous = peak.Load() {
								if peak.CompareAndSwap(previous, n) {
									break
								}
							}
							select {
							case <-mapCtx.Done():
								return mapCtx.Err()
							case <-time.After(50 * time.Millisecond):
								return nil
							}
						})
					}
					if err := group.Wait(); err != nil {
						return err
					}
					// The experiment deliberately does not call finalizeWikiSubtask:
					// no actual Wiki page was generated or published.
					if err := svc.trimPendingList(owner, ids); err != nil {
						return err
					}
					batches++
				}
			})
			require.NoError(t, err)
			require.Equal(t, 4/size, batches)
			require.EqualValues(t, size, peak.Load())
			require.Zero(t, client.Exists(ctx, "wiki:proof:generation:"+kb).Val())
			var counters int64
			require.NoError(t, db.Table("knowledges").Where("pending_subtasks_count=1 AND parse_status='finalizing'").Count(&counters).Error)
			require.EqualValues(t, 4, counters)
			var remaining []types.TaskPendingOp
			require.NoError(t, db.Find(&remaining).Error)
			require.Len(t, remaining, 1)
			require.Equal(t, other.ID, remaining[0].ID)
			require.Nil(t, remaining[0].ClaimedAt)
			t.Logf("controlled scheduling only: batch=%d map=%d batches=%d peak=%d elapsed=%s; no publication or model-throughput claim", size, size, batches, peak.Load(), time.Since(started))
		})
	}
}
