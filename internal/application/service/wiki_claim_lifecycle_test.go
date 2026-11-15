package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/common/wikilock"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWikiClaimAsynqShutdownPostgres(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}))
	require.NoError(t, db.Exec(
		"CREATE TABLE knowledge_bases (id text PRIMARY KEY, tenant_id bigint, deleted_at timestamptz)",
	).Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id,tenant_id) VALUES ('shutdown-kb',1)").Error)
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION", "true")
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_KBS", "shutdown-kb")
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_ACTIVE_FILE", "")
	repo := repository.NewTaskPendingOpsRepository(db)
	priorStamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	row := &types.TaskPendingOp{
		TenantID: 1, TaskType: wikiTaskType, Scope: wikiTaskScope, ScopeID: "shutdown-kb",
		Op: "ingest", DedupKey: "doc", Payload: []byte(`{"op":"ingest","knowledge_id":"doc"}`), FailCount: 2,
		ClaimedAt: &priorStamp,
	}
	require.NoError(t, db.Create(row).Error)
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	options := asynq.RedisClientOpt{Addr: redisServer.Addr()}
	queue := asynq.NewClient(options)
	t.Cleanup(func() { _ = queue.Close() })
	worker := asynq.NewServer(options, asynq.Config{
		Concurrency: 1, Queues: map[string]int{"wiki-shutdown": 1},
		ShutdownTimeout: time.Second, TaskCheckInterval: 10 * time.Millisecond,
	})
	entered := make(chan context.Context, 1)
	returnModel := make(chan struct{})
	returned := make(chan error, 1)
	var old context.Context
	defer func() {
		close(returnModel)
		worker.Shutdown()
		if old != nil {
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Error("retired model handler failed to finish")
			}
		}
	}()
	require.NoError(t, worker.Start(asynq.HandlerFunc(func(ctx context.Context, _ *asynq.Task) error {
		err := withWorkerWikiGenerationLease(ctx, client, "shutdown-kb", func(locked context.Context) error {
			lifecycle := &wikiClaimLifecycle{}
			defer lifecycle.finish()
			locked = context.WithValue(types.WithExecutionTenant(locked, 1), wikiClaimLifecycleKey{}, lifecycle)
			recovery := repo.(interface {
				RecoverWikiClaimsForGeneration(context.Context, uint64, string) (int64, error)
			})
			if n, err := recovery.RecoverWikiClaimsForGeneration(locked, 1, "shutdown-kb"); err != nil || n != 1 {
				return fmt.Errorf("prior generation recovery count=%d: %w", n, err)
			}
			s := &wikiIngestService{pendingRepo: repo}
			_, ids, claims, err := s.claimPendingListWithOwnership(locked, "shutdown-kb", 1)
			if err != nil || len(ids) != 1 {
				return fmt.Errorf("claim failed ids=%v: %w", ids, err)
			}
			entered <- types.WithWikiClaimOwner(locked, 1, "shutdown-kb", claims)
			<-returnModel // Model adapter deliberately ignores cancellation.
			return locked.Err()
		})
		returned <- err
		return err
	})))
	_, err := queue.Enqueue(asynq.NewTask("wiki-shutdown", nil), asynq.Queue("wiki-shutdown"))
	require.NoError(t, err)
	select {
	case old = <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not claim")
	}
	worker.Shutdown()
	require.Eventually(t, func() bool {
		var current types.TaskPendingOp
		released := db.First(&current, "id=?", row.ID).Error == nil && current.ClaimedAt == nil
		return released && !redisServer.Exists("wiki:proof:generation:shutdown-kb")
	}, 3*time.Second, 10*time.Millisecond)
	select {
	case err := <-returned:
		t.Fatalf("model unexpectedly returned before release: %v", err)
	default:
	}
	replacementCtx := context.Background()
	err = withWorkerWikiGenerationLease(replacementCtx, client, "shutdown-kb", func(locked context.Context) error {
		current, claimErr := repo.ClaimBatch(locked, wikiTaskType, wikiTaskScope,
			"shutdown-kb", 1, time.Now().Add(-time.Hour))
		require.NoError(t, claimErr)
		require.Len(t, current, 1)
		stale := context.WithoutCancel(old)
		_, staleErr := repo.IncrFailCount(stale, row.ID)
		require.ErrorIs(t, staleErr, types.ErrWikiClaimChanged)
		require.NoError(t, repo.DeleteByIDs(stale, []int64{row.ID}))
		var kept types.TaskPendingOp
		require.NoError(t, db.First(&kept, "id=?", row.ID).Error)
		require.Equal(t, current[0].ClaimedAt, kept.ClaimedAt)
		require.Equal(t, 2, kept.FailCount)
		require.Equal(t, []byte(row.Payload), []byte(kept.Payload))
		return nil
	})
	require.NoError(t, err)
}

func TestWikiClaimLifecycleCancelsBeforeHandlerReturns(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	repo := repository.NewTaskPendingOpsRepository(f.DB)
	row := &types.TaskPendingOp{
		TenantID: 1, TaskType: wikiTaskType, Scope: wikiTaskScope, ScopeID: "public-two",
		Op: "ingest", DedupKey: "public-two-doc",
		Payload: []byte(`{"op":"ingest","knowledge_id":"public-two-doc"}`), FailCount: 3,
	}
	require.NoError(t, repo.Enqueue(context.Background(), row))
	ctx, cancel := context.WithCancel(types.WithExecutionTenant(context.Background(), 1))
	defer cancel()
	lifecycle := &wikiClaimLifecycle{}
	ctx = context.WithValue(ctx, wikiClaimLifecycleKey{}, lifecycle)
	s := &wikiIngestService{pendingRepo: repo}
	_, ids, claims, err := s.claimPendingListWithOwnership(ctx, "public-two", 1)
	require.NoError(t, err)
	require.Equal(t, []int64{row.ID}, ids)
	require.Len(t, claims, 1)
	cancel() // The handler/model has deliberately not returned yet.
	require.Eventually(t, func() bool {
		var current types.TaskPendingOp
		return f.DB.First(&current, "id=?", row.ID).Error == nil && current.ClaimedAt == nil
	}, 2*time.Second, 10*time.Millisecond)
	newOwner, err := repo.ClaimBatch(types.WithExecutionTenant(context.Background(), 1), wikiTaskType, wikiTaskScope,
		"public-two", 1, time.Now().Add(-90*time.Minute))
	require.NoError(t, err)
	require.Len(t, newOwner, 1)
	lifecycle.finish() // Late handler exit cannot release the replacement.
	var current types.TaskPendingOp
	require.NoError(t, f.DB.First(&current, "id=?", row.ID).Error)
	require.Equal(t, newOwner[0].ClaimedAt, current.ClaimedAt)
	require.Equal(t, 3, current.FailCount)
	require.Equal(t, []byte(row.Payload), []byte(current.Payload))
}

func TestWikiClaimLifecycleNormalEarlyExitRetainsIntent(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	repo := repository.NewTaskPendingOpsRepository(f.DB)
	row := &types.TaskPendingOp{
		TenantID: 1, TaskType: wikiTaskType, Scope: wikiTaskScope, ScopeID: "public-two",
		Op: "ingest", DedupKey: "public-two-doc", Payload: []byte(`{"op":"ingest","knowledge_id":"public-two-doc"}`),
	}
	require.NoError(t, repo.Enqueue(context.Background(), row))
	lifecycle := &wikiClaimLifecycle{}
	ctx := context.WithValue(types.WithExecutionTenant(context.Background(), 1), wikiClaimLifecycleKey{}, lifecycle)
	s := &wikiIngestService{pendingRepo: repo}
	_, _, _, err := s.claimPendingListWithOwnership(ctx, "public-two", 1)
	require.NoError(t, err)
	lifecycle.finish()
	var current types.TaskPendingOp
	require.NoError(t, f.DB.First(&current, "id=?", row.ID).Error)
	require.Nil(t, current.ClaimedAt)
	require.Equal(t, []byte(row.Payload), []byte(current.Payload))
}

func TestWikiClaimGenerationRecoveryFencesOldSettlement(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION", "true")
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_KBS", "public-two")
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_ACTIVE_FILE", "")
	repo := repository.NewTaskPendingOpsRepository(f.DB)
	ctx := types.WithExecutionTenant(context.Background(), 1)
	stamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	row := &types.TaskPendingOp{
		TenantID: 1, TaskType: wikiTaskType, Scope: wikiTaskScope, ScopeID: "public-two",
		Op: "ingest", DedupKey: "public-two-doc", Payload: []byte(`{"op":"ingest","knowledge_id":"public-two-doc"}`),
		ClaimedAt: &stamp, FailCount: 4,
	}
	require.NoError(t, f.DB.Create(row).Error)
	var unrelated []*types.TaskPendingOp
	for _, spec := range []struct {
		tenant          uint64
		kind, scope, kb string
	}{
		{99, wikiTaskType, wikiTaskScope, "public-two"},
		{1, "wiki:finalize", wikiTaskScope, "public-two"},
		{1, wikiTaskType, "other-scope", "public-two"},
		{1, wikiTaskType, wikiTaskScope, "other-kb"},
	} {
		other := &types.TaskPendingOp{
			TenantID: spec.tenant, TaskType: spec.kind, Scope: spec.scope, ScopeID: spec.kb,
			Op: "ingest", DedupKey: fmt.Sprintf("other-%d-%s-%s-%s", spec.tenant, spec.kind, spec.scope, spec.kb),
			Payload: []byte(`{}`), ClaimedAt: &stamp, FailCount: 7,
		}
		require.NoError(t, f.DB.Create(other).Error)
		unrelated = append(unrelated, other)
	}
	recovery := repo.(interface {
		RecoverWikiClaimsForGeneration(context.Context, uint64, string) (int64, error)
	})
	_, err := recovery.RecoverWikiClaimsForGeneration(ctx, 1, "public-two")
	require.Error(t, err)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	err = wikilock.With(ctx, client, "public-two", func(locked context.Context) error {
		_, e := recovery.RecoverWikiClaimsForGeneration(locked, 99, "public-two")
		require.Error(t, e, "a KB lease is not authority to another tenant's KB")
		t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_KBS", "other-kb")
		n, e := recovery.RecoverWikiClaimsForGeneration(locked, 1, "public-two")
		require.NoError(t, e)
		require.Zero(t, n, "inactive spaces must not be recovered")
		t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_KBS", "public-two")
		n, e = recovery.RecoverWikiClaimsForGeneration(locked, 1, "public-two")
		require.NoError(t, e)
		require.EqualValues(t, 1, n)
		for _, other := range unrelated {
			var kept types.TaskPendingOp
			require.NoError(t, f.DB.First(&kept, "id=?", other.ID).Error)
			require.Equal(t, other.ClaimedAt, kept.ClaimedAt)
			require.Equal(t, 7, kept.FailCount)
		}
		current, e := repo.ClaimBatch(locked, wikiTaskType, wikiTaskScope,
			"public-two", 1, time.Now().Add(-90*time.Minute))
		require.NoError(t, e)
		require.Len(t, current, 1)
		old := types.WithWikiClaimOwner(ctx, 1, "public-two", map[int64]time.Time{row.ID: stamp})
		_, e = repo.IncrFailCount(old, row.ID)
		require.ErrorIs(t, e, types.ErrWikiClaimChanged)
		require.NoError(t, repo.ReleaseByIDs(old, []int64{row.ID}))
		require.NoError(t, repo.DeleteByIDs(old, []int64{row.ID}))
		var kept types.TaskPendingOp
		require.NoError(t, f.DB.First(&kept, "id=?", row.ID).Error)
		require.Equal(t, current[0].ClaimedAt, kept.ClaimedAt)
		require.Equal(t, 4, kept.FailCount)
		require.Equal(t, []byte(row.Payload), []byte(kept.Payload))
		require.NoError(t, server.Set("wiki:proof:generation:public-two", "replacement-token"))
		_, e = recovery.RecoverWikiClaimsForGeneration(locked, 1, "public-two")
		require.Error(t, e)
		return nil
	})
	require.Error(t, err, "replacement token must fence the old generation at release as well")
}

func TestWikiClaimCanceledWorkerYieldsGenerationBeforeModelReturns(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan context.Context, 1)
	returnOld := make(chan struct{})
	oldDone := make(chan error, 1)
	go func() {
		oldDone <- withWorkerWikiGenerationLease(ctx, client, "kb", func(locked context.Context) error {
			entered <- locked
			<-returnOld
			return locked.Err()
		})
	}()
	oldContext := <-entered
	cancel()
	require.Eventually(t, func() bool {
		return !server.Exists("wiki:proof:generation:kb")
	}, time.Second, time.Millisecond)
	cleanup, done := wikilock.Detached(oldContext, time.Second)
	require.Error(t, cleanup.Err(), "a canceled worker loses detached publication/settlement authority")
	done()
	newEntered := make(chan struct{})
	returnNew := make(chan struct{})
	newDone := make(chan error, 1)
	go func() {
		newDone <- withWorkerWikiGenerationLease(context.Background(), client, "kb", func(context.Context) error {
			close(newEntered)
			<-returnNew
			return nil
		})
	}()
	<-newEntered
	replacement, err := server.Get("wiki:proof:generation:kb")
	require.NoError(t, err)
	close(returnOld)
	require.Error(t, <-oldDone)
	current, err := server.Get("wiki:proof:generation:kb")
	require.NoError(t, err)
	require.Equal(t, replacement, current, "late old-worker cleanup cannot delete a replacement lease")
	close(returnNew)
	require.NoError(t, <-newDone)
}
