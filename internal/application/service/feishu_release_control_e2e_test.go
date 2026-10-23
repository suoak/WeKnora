package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type releaseHTTPConnector struct {
	deletedItemConnector
	endpoint string
	release  <-chan struct{}
}

func (c *releaseHTTPConnector) Type() string { return types.ConnectorTypeFeishuDrive }
func (c *releaseHTTPConnector) FetchAll(ctx context.Context, _ *types.DataSourceConfig,
	_ []string,
) ([]types.FetchedItem, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint,
		strings.NewReader(`{"input":["synthetic isolated chunk"]}`))
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: feishusafety.Transport{Embedding: true}}
	response, err := client.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		// Simulate slow connector cancellation cleanup, so the controller must
		// fence the live handler before it is allowed to write a stale result.
		<-c.release
		return nil, context.Canceled
	}
	return nil, nil
}

// TestFeishuReleaseStopRedisPostgres runs the actual APP sync handler through
// Asynq/Redis7, PostgreSQL, two controllers and real HTTP. No production fallback
// or live Feishu/gateway endpoint is permitted.
func TestFeishuReleaseStopRedisPostgres(t *testing.T) {
	addr := os.Getenv("FEISHU_SAFETY_REDIS_ADDR")
	if addr == "" {
		if os.Getenv("REQUIRE_SYNC_EXECUTION_POSTGRES") == "1" {
			t.Fatal("dedicated loopback Redis required for end-to-end stop proof")
		}
		t.Skip("isolated Redis/PostgreSQL not configured")
	}
	require.Equal(t, "127.0.0.1:6379", addr)
	db := syncdb.Open(t, "postgres")
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	t.Cleanup(func() { _ = rdb.Close() })
	// Database 15 is reserved for this disposable CI fixture; never FLUSHDB.
	require.Zero(t, rdb.LLen(ctx, "asynq:{sync}:active").Val())
	require.Zero(t, rdb.LLen(ctx, "asynq:{sync}:pending").Val())
	require.Zero(t, rdb.Exists(ctx, "asynq:{sync}:paused").Val())
	sources, logs := repository.NewDataSourceRepository(db), repository.NewSyncLogRepository(db)
	ds := &types.DataSource{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "release-kb",
		Type: types.ConnectorTypeFeishuDrive, Status: types.DataSourceStatusActive,
		Config: types.JSON(`{"type":"feishu_drive"}`), SyncMode: types.SyncModeFull,
		SyncSchedule: "0 0 */6 * * *", LastSyncCursor: types.JSON(`{"checkpoint":"keep"}`),
		LastSyncResult: types.JSON(`{"created":7}`),
	}
	require.NoError(t, sources.Create(ctx, ds))
	t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "true")
	t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "false")
	t.Setenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS", "none")
	feishusafety.InstallDefault(nil)
	t.Cleanup(func() { feishusafety.InstallDefault(nil) })
	svc := &DataSourceService{dsRepo: sources, syncLogRepo: logs}
	old := &types.SyncLog{
		ID: uuid.NewString(), TenantID: 1, DataSourceID: ds.ID,
		Status: types.SyncLogStatusRunning, StartedAt: time.Now().UTC(),
	}
	require.NoError(t, logs.Create(ctx, old))
	body, err := json.Marshal(types.DataSourceSyncPayload{
		TenantID: 1, DataSourceID: ds.ID, SyncLogID: old.ID, Trigger: "schedule", ForceFull: true,
	})
	require.NoError(t, err)
	require.NoError(t, svc.ProcessSync(ctx, asynq.NewTask(types.TypeDataSourceSync, body)))
	stored, err := logs.FindByID(ctx, old.ID)
	require.NoError(t, err)
	require.Equal(t, types.SyncLogStatusCanceled, stored.Status, "scope=none cancels an old delivery before KB access")

	entered, networkCanceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var enteredOnce, canceledOnce, releasedOnce sync.Once
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/online" {
			w.WriteHeader(http.StatusOK)
			return
		}
		enteredOnce.Do(func() { close(entered) })
		<-r.Context().Done()
		canceledOnce.Do(func() { close(networkCanceled) })
	}))
	t.Cleanup(gateway.Close)
	u, err := url.Parse(gateway.URL)
	require.NoError(t, err)
	policy := feishusafety.Config{
		Namespace: "e2e-" + uuid.NewString(), EndpointHost: u.Host,
		InFlight: 1, RPM: 10, RPS: 1, TPM: 10000, WaitBudgetDuration: 5 * time.Second, TokenBoundVerified: true,
	}
	guard, err := feishusafety.New(rdb, policy)
	require.NoError(t, err)
	otherGuard, err := feishusafety.New(rdb, policy)
	require.NoError(t, err)
	feishusafety.InstallDefault(guard)
	t.Setenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS", ds.ID)
	stop := func(stopCtx context.Context) error {
		_, fenceErr := repository.FenceLegacySyncExecutions(stopCtx, db)
		if fenceErr == nil {
			select {
			case <-entered:
				releasedOnce.Do(func() { close(release) })
			default:
			}
		}
		return fenceErr
	}
	var audited atomic.Int64
	audit := func(event feishusynccontrol.Event) error {
		if event.Fenced && event.QueuePaused {
			audited.Add(1)
		}
		return nil
	}
	config := feishusynccontrol.Config{
		WorkerID: "app-a", ExpectedWorkers: []string{"app-a", "app-b"},
		SourceSHA: strings.Repeat("a", 40), Scope: ds.ID, Maintenance: 1,
	}
	a, err := feishusynccontrol.New(rdb, guard, config, stop, audit)
	require.NoError(t, err)
	config.WorkerID = "app-b"
	b, err := feishusynccontrol.New(rdb, otherGuard, config, stop, audit)
	require.NoError(t, err)
	var unhealthy atomic.Bool
	runCtx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	for _, controller := range []*feishusynccontrol.Controller{a, b} {
		workers.Add(1)
		go func(c *feishusynccontrol.Controller) { defer workers.Done(); c.Run(runCtx) }(controller)
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			serverTime, clockErr := rdb.Time(runCtx).Result()
			if clockErr == nil {
				sample, _ := json.Marshal(map[string]any{
					"observed_unix_ms":     serverTime.UnixMilli(),
					"gateway_budget_known": true, "gateway_healthy": true, "online_healthy": true,
					"scope_violations": 0, "quota_exceeded": unhealthy.Load(), "online_p95_ratio": 1.0,
				})
				_ = rdb.Set(runCtx, a.MetricsKey(), sample, time.Second).Err()
			}
			select {
			case <-runCtx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
	t.Cleanup(func() { cancel(); workers.Wait(); releasedOnce.Do(func() { close(release) }) })
	require.Eventually(t, func() bool { return guard.Check(ctx) == nil }, 4*time.Second, 20*time.Millisecond)
	// Only the isolated fixture explicitly resumes its dedicated test queue.
	// The controller never resumes consumption, including after healthy samples.
	require.NoError(t, rdb.Del(ctx, "asynq:{sync}:paused").Err())
	options := asynq.RedisClientOpt{Addr: addr, DB: 15}
	client := asynq.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(&releaseHTTPConnector{endpoint: gateway.URL, release: release}))
	svc.taskEnqueuer, svc.connectorRegistry = client, registry
	svc.kbService = &processSyncKBService{kb: &types.KnowledgeBase{ID: ds.KnowledgeBaseID, TenantID: 1}}
	svc.tenantRepo, svc.tagService = &processSyncTenantRepo{tenant: &types.Tenant{ID: 1}}, &processSyncTagService{}
	server := asynq.NewServer(options, asynq.Config{
		Concurrency: 1,
		Queues:      map[string]int{types.QueueSync: 2, types.QueueMaintenance: 1}, ShutdownTimeout: time.Second,
	})
	mux := asynq.NewServeMux()
	mux.HandleFunc(types.TypeDataSourceSync, svc.ProcessSync)
	var low atomic.Int64
	mux.HandleFunc("release:low", func(context.Context, *asynq.Task) error { low.Add(1); return nil })
	require.NoError(t, server.Start(mux))
	t.Cleanup(server.Shutdown)
	run, err := svc.ManualSync(ctx, ds.ID)
	require.NoError(t, err)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("actual Asynq handler did not reach isolated HTTP gateway")
	}
	claimed, err := logs.FindByID(ctx, run.ID)
	require.NoError(t, err)
	require.True(t, claimed.ExecutionClaimed)
	owner := types.SyncExecution{
		TenantID: 1, DataSourceID: ds.ID, SyncLogID: run.ID,
		Generation: claimed.ExecutionGeneration,
	}
	unhealthy.Store(true) // monitoring fixture, not a direct controller stop call
	require.Eventually(t, func() bool { return rdb.Exists(ctx, "asynq:{sync}:paused").Val() == 1 },
		4*time.Second, 20*time.Millisecond)
	select {
	case <-networkCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight HTTP did not observe automatic stop")
	}
	require.Eventually(t, func() bool {
		log, readErr := logs.FindByID(ctx, run.ID)
		return readErr == nil && !log.ExecutionClaimed && log.Status != types.SyncLogStatusRunning
	}, 3*time.Second, 20*time.Millisecond)
	savedSource, err := sources.FindByID(ctx, ds.ID)
	require.NoError(t, err)
	savedLog, err := logs.FindByID(ctx, run.ID)
	require.NoError(t, err)
	require.Greater(t, savedLog.ExecutionGeneration, owner.Generation)
	valid, err := logs.(interface {
		WriteExecution(context.Context, types.SyncExecution, *types.DataSource, *types.SyncLog, bool) (bool, error)
	}).WriteExecution(ctx, owner, savedSource, claimed, true)
	require.NoError(t, err)
	require.False(t, valid, "old generation must not publish or write a checkpoint")
	require.JSONEq(t, string(ds.LastSyncCursor), string(savedSource.LastSyncCursor))
	require.JSONEq(t, string(ds.LastSyncResult), string(savedSource.LastSyncResult))
	require.Equal(t, ds.SyncSchedule, savedSource.SyncSchedule)
	require.NotEqual(t, types.DataSourceStatusPaused, savedSource.Status)
	require.Eventually(t, func() bool { return audited.Load() > 0 }, time.Second, 20*time.Millisecond)
	_, err = client.Enqueue(asynq.NewTask("release:low", nil), asynq.Queue(types.QueueMaintenance))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return low.Load() == 1 }, 3*time.Second, 20*time.Millisecond)
	// The same transport still allows unmarked online traffic after the latch.
	online := &http.Client{Transport: feishusafety.Transport{Embedding: true}}
	response, err := online.Get(gateway.URL + "/online")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Error(t, guard.Check(ctx))
}
