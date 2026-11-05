package router

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeExecutionRequiresActiveMembershipAndLiveLease(t *testing.T) {
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	client := asynq.NewClientFromRedisClient(rc)
	t.Cleanup(func() { _ = client.Close() })
	probe := &asynqTaskInspector{inspector: asynq.NewInspectorFromRedisClient(rc), redis: rc}
	ctx := context.Background()
	enqueueTask(t, client, types.TypeDocumentProcess, `{"knowledge_id":"document"}`, "work")
	leases, err := probe.KnowledgeExecutionLeases(ctx, []string{"document"})
	require.NoError(t, err)
	require.Empty(t, leases, "pending does not mean executing")
	prefix := "asynq:{" + types.QueueDefault + "}:"
	require.NoError(t, rc.LRem(ctx, prefix+"pending", 0, "work").Err())
	require.NoError(t, rc.LPush(ctx, prefix+"active", "work").Err())
	require.NoError(t, rc.HSet(ctx, prefix+"t:work", "state", "active").Err())
	live := redis.Z{Member: "work", Score: float64(time.Now().Add(time.Minute).Unix())}
	require.NoError(t, rc.ZAdd(ctx, prefix+"lease", live).Err())
	leases, err = probe.KnowledgeExecutionLeases(ctx, []string{"document", "unrelated"})
	require.NoError(t, err)
	require.Contains(t, leases, "document")
	require.NotContains(t, leases, "unrelated")
	expired := redis.Z{Member: "work", Score: float64(time.Now().Add(-time.Minute).Unix())}
	require.NoError(t, rc.ZAdd(ctx, prefix+"lease", expired).Err())
	leases, err = probe.KnowledgeExecutionLeases(ctx, []string{"document"})
	require.NoError(t, err)
	require.Empty(t, leases, "expired active record is a lost worker")
}
