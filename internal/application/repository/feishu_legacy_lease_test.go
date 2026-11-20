package repository

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestFeishuLegacyLeaseRenewPostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	tenant := &types.Tenant{ID: 809933, Name: "lease-renew-private", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "lease", Type: "document"}
	require.NoError(t, db.Create(kb).Error)
	ds := &types.DataSource{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Type: "feishu_drive",
		Status: "active", Config: types.JSON(`{}`),
	}
	require.NoError(t, db.Create(ds).Error)
	log := &types.SyncLog{
		ID: uuid.NewString(), DataSourceID: ds.ID, TenantID: tenant.ID, Status: "running",
		ExecutionGeneration: 1, ExecutionClaimed: true,
	}
	require.NoError(t, db.Create(log).Error)
	ctx := types.WithSyncExecution(context.Background(), types.SyncExecution{
		DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: tenant.ID, Generation: 1,
	})
	store := NewFeishuLegacyStore(db)
	item := &types.FetchedItem{
		ExternalID: "renew-source", FileName: "source.md", Title: "source",
		Content: []byte("private candidate"), UpdatedAt: time.Now().UTC(),
	}
	resource, owner, err := store.Begin(ctx, ds, item, LegacySourceHash(item.Content, item.FileName, item.Title))
	require.NoError(t, err)
	// Large private checkpoints must stay intact on disk but must not be fetched
	// into the application for a lease renewal, including concurrent renewals.
	payload, err := json.Marshal(map[string]string{"private": strings.Repeat("x", 1<<20)})
	require.NoError(t, err)
	require.NoError(t, db.Model(owner).Updates(map[string]any{
		"manifest": json.RawMessage(payload), "receipt": json.RawMessage(payload),
	}).Error)
	type leaseQueryKey struct{}
	ctx = context.WithValue(ctx, leaseQueryKey{}, true)
	var metadataReads atomic.Int32
	callback := "test:lease_without_private_payload"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		if query.Statement.Context.Value(leaseQueryKey{}) != true ||
			query.Statement.Table != "feishu_legacy_attempts" {
			return
		}
		a, ok := query.Statement.Dest.(*types.FeishuLegacyAttempt)
		if !ok || len(a.Manifest) != 0 || len(a.Receipt) != 0 {
			_ = query.AddError(errors.New("lease query fetched private checkpoint payload"))
			return
		}
		metadataReads.Add(1)
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Query().Remove(callback)) })
	load := func() types.FeishuLegacyAttempt {
		var a types.FeishuLegacyAttempt
		require.NoError(t, db.First(&a, "id=?", owner.ID).Error)
		return a
	}
	var resourceBefore types.FeishuLegacyResource
	require.NoError(t, db.First(&resourceBefore, "id=?", resource.ID).Error)
	before := load()
	require.NoError(t, store.Renew(ctx, ds, owner))
	require.EqualValues(t, 1, metadataReads.Load())
	after := load()
	require.False(t, after.LeaseUntil.Before(before.LeaseUntil))
	after.LeaseUntil = before.LeaseUntil
	require.Equal(t, before, after, "renew changes only the lease")
	var unchanged types.FeishuLegacyResource
	require.NoError(t, db.First(&unchanged, "id=?", resource.ID).Error)
	require.Equal(t, resourceBefore, unchanged, "no resource watermark/generation/active-publication change")
	// Concurrent renewals do not mutate their shared owner pointer or regress a lease.
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- store.Renew(ctx, ds, owner) }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.EqualValues(t, 9, metadataReads.Load())
	// Reproduce a long operation crossing a real PostgreSQL deadline without
	// waiting twenty minutes. Cached client lease values cannot override DB time.
	require.NoError(t, db.Model(owner).UpdateColumn("lease_until",
		gorm.Expr("clock_timestamp()+interval '1 second'")).Error)
	time.Sleep(1100 * time.Millisecond)
	require.ErrorIs(
		t,
		store.CheckOwner(context.WithValue(ctx, leaseQueryKey{}, false), ds, owner),
		ErrLegacyVersionFence,
	)
	require.NoError(t, db.Model(owner).UpdateColumn("lease_until",
		gorm.Expr("clock_timestamp()+interval '1 second'")).Error)
	require.NoError(t, store.Renew(ctx, ds, owner))
	time.Sleep(1100 * time.Millisecond)
	require.NoError(t, store.CheckOwner(context.WithValue(ctx, leaseQueryKey{}, false), ds, owner),
		"live fenced progress extends the deadline")
	for _, kind := range []string{"nonce", "generation", "claim", "paused", "revoked", "expired", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			require.NoError(t, db.Model(ds).UpdateColumn("status", "active").Error)
			require.NoError(t, db.Model(log).UpdateColumn("execution_claimed", true).Error)
			require.NoError(t, db.Model(resource).UpdateColumn("state", "active").Error)
			require.NoError(t, db.Model(owner).UpdateColumn("lease_until",
				gorm.Expr("clock_timestamp()+interval '20 minutes'")).Error)
			copyOwner := *owner
			checkCtx := ctx
			switch kind {
			case "nonce":
				copyOwner.OwnerNonce += "wrong"
			case "generation":
				copyOwner.Generation++
			case "claim":
				require.NoError(t, db.Model(log).UpdateColumn("execution_claimed", false).Error)
			case "paused":
				require.NoError(t, db.Model(ds).UpdateColumn("status", "paused").Error)
			case "revoked":
				require.NoError(t, db.Model(resource).UpdateColumn("state", "revoked").Error)
			case "expired":
				require.NoError(t, db.Model(owner).UpdateColumn("lease_until",
					gorm.Expr("clock_timestamp()-interval '1 second'")).Error)
			case "canceled":
				var cancel context.CancelFunc
				checkCtx, cancel = context.WithCancel(ctx)
				cancel()
			}
			beforeFailure := load()
			require.Error(t, store.Renew(checkCtx, ds, &copyOwner))
			require.Equal(t, beforeFailure, load(), "invalid owners cannot revive or extend a lease")
		})
	}
}
