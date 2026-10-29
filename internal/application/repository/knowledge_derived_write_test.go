package repository

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
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

// Uses the real migrator, epoch triggers and source/content fence in a fresh
// disposable PostgreSQL database. Never accepts a shared application DSN.
func TestKnowledgeDerivedWritePostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	db = types.EnableLegacyReadGuards(db)
	repo := &knowledgeRepository{db: db}
	tenant := &types.Tenant{ID: 809943, Name: "derived-write-test", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "derived-write-test", Type: "document"}
	require.NoError(t, db.Create(kb).Error)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenant.ID)
	binder := func(ctx context.Context, _ *types.DataSource) (context.Context, error) {
		return types.WithFeishuSourceCommitGuard(ctx, func(*gorm.DB) error { return nil }), nil
	}
	fixture := func(t *testing.T) (*types.Knowledge, *types.DataSource, string) {
		t.Helper()
		k := &types.Knowledge{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Title: "derived", Type: "file", ParseStatus: "completed", EnableStatus: "enabled", SummaryStatus: "none"}
		require.NoError(t, db.Create(k).Error)
		ds := &types.DataSource{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Name: "test", Type: "feishu_drive", Status: "active", LegacyAccessState: "active", Config: types.JSON(`{}`)}
		require.NoError(t, db.Create(ds).Error)
		attempt := uuid.NewString()
		r := &types.FeishuLegacyResource{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, DataSourceID: ds.ID, ExternalID: uuid.NewString(), KnowledgeID: k.ID, Generation: 1, State: "active", CurrentAttempt: attempt, ActiveAttempt: attempt, QuarantineSnapshot: json.RawMessage(`{}`)}
		require.NoError(t, db.Create(r).Error)
		a := &types.FeishuLegacyAttempt{ID: attempt, ResourceID: r.ID, Generation: 1, OwnerNonce: "expired-test-nonce", LeaseUntil: time.Now().Add(-time.Hour), Status: "published", Revision: "test", SourceHash: "test", ExpectedHash: "", Manifest: json.RawMessage(`{}`), Receipt: json.RawMessage(`{}`)}
		require.NoError(t, db.Create(a).Error)
		v, managed, err := repo.KnowledgeDerivedVersion(ctx, k.ID)
		require.NoError(t, err)
		require.True(t, managed)
		require.NotEmpty(t, v)
		return k, ds, v
	}
	t.Run("published-expired-lease-allows-only-derived-fields", func(t *testing.T) {
		k, _, v := fixture(t)
		bound, managed, err := repo.BeginKnowledgeDerivedWrite(ctx, k.ID, tenant.ID, v, binder)
		require.NoError(t, err)
		require.True(t, managed)
		require.NoError(t, repo.UpdateKnowledgeColumn(bound, k.ID, "summary_status", "processing"))
		require.NoError(t, repo.UpdateKnowledgeColumns(bound, k.ID, map[string]interface{}{"description": "new summary", "summary_status": "completed", "profile": &types.KnowledgeProfile{Gist: "derived gist"}}))
		require.NoError(t, repo.UpdateKnowledgeColumn(bound, k.ID, "metadata", types.JSON(`{"derived":{"summary_language":"zh"}}`)))
		require.ErrorIs(t, repo.UpdateKnowledgeColumn(bound, k.ID, "parse_status", "pending"), ErrDerivedWriteRejected)
		require.Error(t, repo.UpdateKnowledgeColumn(bound, k.ID, "metadata", types.JSON(`{"datasource_id":"foreign"}`)))
		var tickets int64
		require.NoError(t, db.Table("feishu_derived_write_grants").Count(&tickets).Error)
		require.Zero(t, tickets)
		// A grant for metadata must not weaken the ordinary publication fence.
		require.Error(t, db.Exec("UPDATE knowledges SET description='unmanaged' WHERE id=?", k.ID).Error)
		require.Error(t, db.Exec("UPDATE knowledges SET parse_status='pending' WHERE id=?", k.ID).Error)
		require.Error(t, db.Create(&types.Chunk{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeID: k.ID, KnowledgeBaseID: kb.ID, Content: "unmanaged", ChunkType: types.ChunkTypeText}).Error)
	})
	t.Run("competing-snapshots-cannot-overwrite", func(t *testing.T) {
		k, _, v := fixture(t)
		first, _, err := repo.BeginKnowledgeDerivedWrite(ctx, k.ID, tenant.ID, v, binder)
		require.NoError(t, err)
		second, _, err := repo.BeginKnowledgeDerivedWrite(ctx, k.ID, tenant.ID, v, binder)
		require.NoError(t, err)
		require.NoError(t, repo.UpdateKnowledgeColumn(first, k.ID, "description", "newer"))
		require.ErrorIs(t, repo.UpdateKnowledgeColumn(second, k.ID, "description", "older"), ErrDerivedWriteRejected)
		// A fresh retry for unchanged published content is legal; only the
		// old in-flight optimistic snapshot is stale after derived updates.
		_, _, err = repo.BeginKnowledgeDerivedWrite(ctx, k.ID, tenant.ID, v, binder)
		require.NoError(t, err)
	})
	t.Run("revocation-and-authority-change-are-terminal", func(t *testing.T) {
		k, ds, v := fixture(t)
		bound, _, err := repo.BeginKnowledgeDerivedWrite(ctx, k.ID, tenant.ID, v, binder)
		require.NoError(t, err)
		require.NoError(t, db.Model(&types.DataSource{}).Where("id=?", ds.ID).Update("legacy_access_state", "revoked").Error)
		require.ErrorIs(t, repo.UpdateKnowledgeColumn(bound, k.ID, "description", "denied"), ErrDerivedWriteRejected)
		require.ErrorIs(t, repo.UpdateKnowledgeColumn(ctx, k.ID, "description", "no grant"), ErrDerivedWriteRejected)
		k, _, v = fixture(t)
		_, _, err = repo.BeginKnowledgeDerivedWrite(ctx, k.ID, tenant.ID, v, nil)
		require.ErrorIs(t, err, ErrDerivedWriteRejected)
		_, _, err = repo.BeginKnowledgeDerivedWrite(ctx, k.ID, tenant.ID, "", binder)
		require.ErrorIs(t, err, ErrDerivedWriteRejected)
	})
	t.Run("current-generation-change-rejects-old-task", func(t *testing.T) {
		k, _, v := fixture(t)
		bound, _, err := repo.BeginKnowledgeDerivedWrite(ctx, k.ID, tenant.ID, v, binder)
		require.NoError(t, err)
		require.NoError(t, db.Model(&types.FeishuLegacyResource{}).Where("knowledge_id=?", k.ID).Update("generation", 2).Error)
		require.ErrorIs(t, repo.UpdateKnowledgeColumn(bound, k.ID, "description", "old generation"), ErrDerivedWriteRejected)
	})
	t.Run("missing-epoch-cannot-be-treated-as-unmanaged", func(t *testing.T) {
		k, _, _ := fixture(t)
		require.NoError(t, db.Exec("DELETE FROM document_access_epochs WHERE knowledge_id=?", k.ID).Error)
		_, managed, err := repo.KnowledgeDerivedVersion(ctx, k.ID)
		require.True(t, managed)
		require.ErrorIs(t, err, ErrDerivedWriteRejected)
	})
	t.Run("concurrent-results-have-one-winner", func(t *testing.T) {
		k, _, v := fixture(t)
		first, _, err := repo.BeginKnowledgeDerivedWrite(ctx, k.ID, tenant.ID, v, binder)
		require.NoError(t, err)
		second, _, err := repo.BeginKnowledgeDerivedWrite(ctx, k.ID, tenant.ID, v, binder)
		require.NoError(t, err)
		start := make(chan struct{})
		results := make(chan error, 2)
		var workers sync.WaitGroup
		for _, bound := range []context.Context{first, second} {
			workers.Add(1)
			go func(bound context.Context) {
				defer workers.Done()
				<-start
				results <- repo.UpdateKnowledgeColumn(bound, k.ID, "description", "concurrent result")
			}(bound)
		}
		close(start)
		workers.Wait()
		close(results)
		won, lost := 0, 0
		for err := range results {
			if err == nil {
				won++
			} else {
				require.ErrorIs(t, err, ErrDerivedWriteRejected)
				lost++
			}
		}
		require.Equal(t, 1, won)
		require.Equal(t, 1, lost)
	})
	t.Run("rollback-restores-original-fence-and-can-reapply", func(t *testing.T) {
		k, _, _ := fixture(t)
		down, err := os.ReadFile("migrations/versioned/003043_feishu_derived_write.down.sql")
		require.NoError(t, err)
		up, err := os.ReadFile("migrations/versioned/003043_feishu_derived_write.up.sql")
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(down)).Error)
		require.Error(t, db.Exec("UPDATE knowledges SET description='old worker' WHERE id=?", k.ID).Error)
		require.NoError(t, db.Exec(string(up)).Error)
		v, _, err := repo.KnowledgeDerivedVersion(ctx, k.ID)
		require.NoError(t, err)
		bound, _, err := repo.BeginKnowledgeDerivedWrite(ctx, k.ID, tenant.ID, v, binder)
		require.NoError(t, err)
		require.NoError(t, repo.UpdateKnowledgeColumn(bound, k.ID, "description", "after reapply"))
	})
}
