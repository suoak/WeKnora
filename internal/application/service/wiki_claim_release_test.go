package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/common/redislock"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWikiClaimReleaseCAS(t *testing.T) {
	testWikiClaimReleaseCAS(t, fixture.New(t))
}

func testWikiClaimReleaseCAS(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db := f.DB
	ctx := types.WithExecutionTenant(context.Background(), 1)
	repo := repository.NewTaskPendingOpsRepository(db)
	releaser := repo.(wikiOwnedClaimReleaser)
	create := func(key, taskType, scope string, claim time.Time) *types.TaskPendingOp {
		t.Helper()
		row := &types.TaskPendingOp{
			TenantID: 1, TaskType: taskType, Scope: scope, ScopeID: "public-two",
			Op: "ingest", DedupKey: key, Payload: []byte(`{"knowledge_id":"public-two-doc","attempt":7}`),
			FailCount: 4, ClaimedAt: &claim,
		}
		require.NoError(t, db.Create(row).Error)
		require.NoError(t, db.First(row, "id=?", row.ID).Error)
		return row
	}
	old := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	row := create("claim-old", types.TypeWikiIngest, types.TaskScopeKnowledgeBase, old)
	beforePayload := append([]byte(nil), row.Payload...)
	claims := map[int64]time.Time{row.ID: *row.ClaimedAt}
	n, err := releaser.ReleaseWikiClaimsIfOwned(ctx, 99, "public-two", claims)
	require.NoError(t, err)
	require.Zero(t, n)
	n, err = releaser.ReleaseWikiClaimsIfOwned(ctx, 1, "different-kb", claims)
	require.NoError(t, err)
	require.Zero(t, n)

	// Reclaim through the actual grouped ClaimBatch transaction. Late cleanup
	// from the old attempt must leave the new SQL owner untouched.
	newOwner, err := repo.ClaimBatch(ctx, types.TypeWikiIngest, types.TaskScopeKnowledgeBase,
		"public-two", 1, time.Now().Add(-90*time.Minute))
	require.NoError(t, err)
	require.Len(t, newOwner, 1)
	require.Equal(t, row.ID, newOwner[0].ID)
	require.NotEqual(t, old, *newOwner[0].ClaimedAt)
	n, err = releaser.ReleaseWikiClaimsIfOwned(ctx, 1, "public-two", claims)
	require.NoError(t, err)
	require.Zero(t, n)
	var current types.TaskPendingOp
	require.NoError(t, db.First(&current, "id=?", row.ID).Error)
	require.Equal(t, newOwner[0].ClaimedAt, current.ClaimedAt)
	claims[row.ID] = *current.ClaimedAt
	n, err = releaser.ReleaseWikiClaimsIfOwned(ctx, 1, "public-two", claims)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	current = types.TaskPendingOp{}
	require.NoError(t, db.First(&current, "id=?", row.ID).Error)
	require.Nil(t, current.ClaimedAt)
	require.Equal(t, beforePayload, []byte(current.Payload))
	require.Equal(t, 4, current.FailCount)
	require.Equal(t, row.ID, current.ID)
	n, err = releaser.ReleaseWikiClaimsIfOwned(ctx, 1, "public-two", claims)
	require.NoError(t, err)
	require.Zero(t, n, "repeated cleanup has no effect")

	for _, row := range []*types.TaskPendingOp{
		create("other-type", "wiki:finalize", types.TaskScopeKnowledgeBase, old),
		create("other-scope", types.TypeWikiIngest, "unrelated_scope", old),
	} {
		n, err = releaser.ReleaseWikiClaimsIfOwned(ctx, 1, "public-two", map[int64]time.Time{row.ID: *row.ClaimedAt})
		require.NoError(t, err)
		require.Zero(t, n)
	}
	_, err = releaser.ReleaseWikiClaimsIfOwned(ctx, 1, "public-two", map[int64]time.Time{row.ID: {}})
	require.Error(t, err)

	if db.Name() == "postgres" {
		row := create("concurrent-release", types.TypeWikiIngest, types.TaskScopeKnowledgeBase, old)
		claims := map[int64]time.Time{row.ID: *row.ClaimedAt}
		results := make(chan int64, 8)
		errors := make(chan error, 8)
		var group sync.WaitGroup
		for range 8 {
			group.Add(1)
			go func() {
				defer group.Done()
				n, err := releaser.ReleaseWikiClaimsIfOwned(ctx, 1, "public-two", claims)
				results <- n
				errors <- err
			}()
		}
		group.Wait()
		close(results)
		close(errors)
		var released int64
		for n := range results {
			released += n
		}
		for err := range errors {
			require.NoError(t, err)
		}
		require.EqualValues(t, 1, released, "only one concurrent cleanup may release the lease")
	}
}

func TestWikiClaimCleanupSurvivesLostGenerationOwnership(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	repo := repository.NewTaskPendingOpsRepository(f.DB)
	row := &types.TaskPendingOp{
		TenantID: 1, TaskType: types.TypeWikiIngest, Scope: types.TaskScopeKnowledgeBase,
		ScopeID: "public-two", Op: "ingest", DedupKey: "public-two-doc", Payload: []byte(`{}`),
	}
	require.NoError(t, repo.Enqueue(context.Background(), row))
	s := &wikiIngestService{pendingRepo: repo}
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	parent, cancel := context.WithCancel(types.WithExecutionTenant(context.Background(), 1))
	defer cancel()
	err := redislock.WithRenewableLock(parent, client, "wiki:proof:generation:public-two",
		time.Second, 10*time.Millisecond, func(locked context.Context) error {
			_, ids, ownership, err := s.claimPendingListWithOwnership(locked, "public-two", 1)
			require.NoError(t, err)
			require.Equal(t, []int64{row.ID}, ids)
			require.Len(t, ownership, 1)
			server.Del("wiki:proof:generation:public-two")
			select {
			case <-locked.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("generation ownership loss was not observed")
			}
			oldCleanup, done := wikiIngestCleanupContext(locked)
			defer done()
			require.Error(t, oldCleanup.Err(), "the old lock-bound cleanup is already canceled")
			released, err := releaseAbortedWikiClaims(locked, repo.(wikiOwnedClaimReleaser),
				WikiIngestPayload{TenantID: 1, KnowledgeBaseID: "public-two"}, ownership)
			require.NoError(t, err)
			require.EqualValues(t, 1, released)
			return locked.Err()
		})
	require.Error(t, err)
	var current types.TaskPendingOp
	require.NoError(t, f.DB.First(&current, "id=?", row.ID).Error)
	require.Nil(t, current.ClaimedAt)
}
