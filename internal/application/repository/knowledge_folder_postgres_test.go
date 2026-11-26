package repository

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Runs the real governed migration chain in a newly allocated disposable DB.
func TestPersistentKnowledgeFoldersPostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	defer pool.Close()
	tenant := &types.Tenant{ID: 809952, Name: "folders synthetic", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "folders", Type: "document"}
	require.NoError(t, db.Create(kb).Error)
	ctx := context.Background()
	repo := NewKnowledgeRepository(db)
	for _, path := range []string{"empty", "empty/child", "empty/child/deep"} {
		require.NoError(t, repo.CreateKnowledgeFolder(ctx, tenant.ID, kb.ID, path))
	}
	// A new connection sees persisted zero-document paths.
	fresh, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	freshPool, err := fresh.DB()
	require.NoError(t, err)
	defer freshPool.Close()
	counts, err := NewKnowledgeRepository(fresh).ListKnowledgeFolderCounts(ctx, tenant.ID, kb.ID)
	require.NoError(t, err)
	tree := types.BuildKnowledgeFolderTree(counts)
	require.Zero(t, tree.TotalDocumentCount)
	require.Equal(t, "empty/child/deep", tree.Folders[0].Children[0].Children[0].Path)
	require.ErrorIs(t, repo.DeleteKnowledgeFolder(ctx, tenant.ID, kb.ID, "empty"), ErrKnowledgeFolderNotEmpty)
	n, err := repo.RenameKnowledgeFolderPath(ctx, tenant.ID, kb.ID, "empty", "renamed")
	require.NoError(t, err)
	require.Zero(t, n, "moved_count counts documents, not directories")
	require.NoError(t, repo.CreateKnowledgeFolder(ctx, tenant.ID, kb.ID, "target"))
	_, err = repo.RenameKnowledgeFolderPath(ctx, tenant.ID, kb.ID, "renamed", "target")
	require.NoError(t, err)
	k := &types.Knowledge{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Type: "file", Title: "unchanged", FileName: "original.md",
		FolderPath: "target/child/deep", ParseStatus: types.ParseStatusCompleted,
	}
	require.NoError(t, db.Create(k).Error)
	var before string
	const documentProjection = "SELECT (to_jsonb(k) - 'folder_path' - 'updated_at')::text " +
		"FROM knowledges k WHERE id=?"
	require.NoError(t, db.Raw(documentProjection, k.ID).Scan(&before).Error)
	n, err = repo.RenameKnowledgeFolderPath(ctx, tenant.ID, kb.ID, "target", "final")
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	var after string
	require.NoError(t, db.Raw(documentProjection, k.ID).Scan(&after).Error)
	require.Equal(t, before, after, "all document fields except placement/timestamp remain identical")
	require.ErrorIs(t, repo.DeleteKnowledgeFolder(ctx, tenant.ID, kb.ID, "final/child/deep"),
		ErrKnowledgeFolderNotEmpty)
	require.NoError(t, db.Delete(k).Error)
	require.NoError(t, repo.DeleteKnowledgeFolder(ctx, tenant.ID, kb.ID, "final/child/deep"))
	require.ErrorIs(t, repo.CreateKnowledgeFolder(ctx, tenant.ID+1, kb.ID, "foreign"), ErrKnowledgeFolderNotFound)
	// Real cross-connection same-path submissions: exactly one winner.
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- repo.CreateKnowledgeFolder(ctx, tenant.ID, kb.ID, "concurrent") }()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, ErrKnowledgeFolderConflict)
		}
	}
	require.Equal(t, 1, winners)
	// Deleting a leaf and creating its child serialize as one of two complete
	// outcomes; a stale parent check must never leave a dangling child marker.
	require.NoError(t, repo.CreateKnowledgeFolder(ctx, tenant.ID, kb.ID, "race-delete"))
	start := make(chan struct{})
	var deleted, created error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		deleted = repo.DeleteKnowledgeFolder(ctx, tenant.ID, kb.ID, "race-delete")
	}()
	go func() {
		defer wg.Done()
		<-start
		created = repo.CreateKnowledgeFolder(ctx, tenant.ID, kb.ID, "race-delete/child")
	}()
	close(start)
	wg.Wait()
	if deleted == nil {
		require.ErrorIs(t, created, ErrKnowledgeFolderNotFound)
	} else {
		require.ErrorIs(t, deleted, ErrKnowledgeFolderNotEmpty)
		require.NoError(t, created)
	}
	// Export markers before destructive down; restore after up. Documents survive both.
	retained := &types.Knowledge{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Type: "file", Title: "retained document", ParseStatus: types.ParseStatusCompleted,
	}
	require.NoError(t, db.Create(retained).Error)
	const fullDocumentProjection = "SELECT to_jsonb(k)::text FROM knowledges k WHERE id=?"
	var retainedBefore, retainedAfter string
	require.NoError(t, db.Raw(fullDocumentProjection, retained.ID).Scan(&retainedBefore).Error)
	var preserved []types.KnowledgeFolder
	require.NoError(t, db.Find(&preserved).Error)
	runner, err := migrate.New("file://migrations/versioned", dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		sourceErr, databaseErr := runner.Close()
		require.NoError(t, sourceErr)
		require.NoError(t, databaseErr)
	})
	require.NoError(t, runner.Migrate(3051))
	require.NoError(t, runner.Migrate(3052))
	require.NoError(t, db.Create(&preserved).Error)
	require.NoError(t, db.Raw(fullDocumentProjection, retained.ID).Scan(&retainedAfter).Error)
	require.Equal(t, retainedBefore, retainedAfter, "formal down/up must not change any document field")
	var paths []string
	require.NoError(t, db.Model(&types.KnowledgeFolder{}).Pluck("path", &paths).Error)
	require.Contains(t, paths, "final/child")
	require.NotContains(t, strings.Join(paths, "|"), "empty")
}
