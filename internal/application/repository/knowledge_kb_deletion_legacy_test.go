package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func protectedDeletionFixture(t *testing.T) (*gorm.DB, *knowledgeRepository) {
	t.Helper()
	db := syncdb.Open(t, "postgres")
	for _, ddl := range []string{
		"CREATE TABLE knowledge_bases(id TEXT PRIMARY KEY,tenant_id BIGINT," +
			"vector_store_id TEXT,deleted_at TIMESTAMPTZ)",
		"CREATE TABLE knowledges(id TEXT PRIMARY KEY,tenant_id BIGINT,knowledge_base_id TEXT," +
			"channel TEXT,metadata JSONB,deleted_at TIMESTAMPTZ)",
		"CREATE TABLE chunks(id TEXT PRIMARY KEY,tenant_id BIGINT,knowledge_base_id TEXT," +
			"knowledge_id TEXT,deleted_at TIMESTAMPTZ,is_enabled BOOLEAN)",
		"CREATE TABLE embeddings(id TEXT PRIMARY KEY,knowledge_base_id TEXT,knowledge_id TEXT)",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	require.NoError(t, db.AutoMigrate(&types.AuditLog{}))
	path := filepath.Join("..", "..", "..", "migrations", "versioned", "003036_feishu_legacy_atomic.up.sql")
	migration, err := os.ReadFile(path)
	require.NoError(t, err)
	sql := string(migration)
	// Use the unchanged production fence, with small disposable table fixtures.
	tables := sql[strings.Index(sql, "CREATE TABLE feishu_legacy_resources"):strings.Index(sql, "-- No candidate")]
	require.NoError(t, db.Exec(tables).Error)
	fence := sql[strings.Index(sql, "CREATE FUNCTION knowhub_feishu_legacy_write_fence"):]
	fence = fence[:strings.Index(fence, "DO $$ BEGIN")]
	require.NoError(t, db.Exec(fence).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER feishu_legacy_embedding_fence BEFORE INSERT OR UPDATE OR DELETE
 ON embeddings FOR EACH ROW EXECUTE FUNCTION knowhub_feishu_legacy_write_fence()`).Error)
	for _, ddl := range []string{
		"INSERT INTO knowledge_bases VALUES('kb',1,NULL,clock_timestamp())",
		"INSERT INTO knowledges VALUES('kid',1,'kb','feishu'," +
			"'{\"datasource_id\":\"ds\",\"external_id\":\"ext\"}',NULL)",
		"INSERT INTO chunks VALUES('chunk',1,'kb','kid',NULL,true)",
		"INSERT INTO embeddings VALUES('vector','kb','kid')",
		"INSERT INTO feishu_legacy_resources(id,tenant_id,knowledge_base_id," +
			"data_source_id,external_id,knowledge_id,state) " +
			"VALUES('resource',1,'kb','ds','ext','kid','revoked')",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	require.NoError(t, db.Create(&types.DataSource{
		ID: "ds", TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu", LegacyAccessState: "revoked",
	}).Error)
	require.NoError(t, db.Exec("UPDATE data_sources SET deleted_at=clock_timestamp() WHERE id='ds'").Error)
	require.NoError(t, db.Create(&types.AuditLog{
		TenantID: 1, ActorUserID: "normal-authorized-deleter", Action: types.AuditActionKBDeleted,
		TargetType: "knowledge_base", TargetID: "kb", Outcome: types.AuditOutcomeSuccess,
	}).Error)
	return db, &knowledgeRepository{db: db}
}

func TestProtectedKBDeletionLeavesOrdinaryLiteDeletionUnchanged(t *testing.T) {
	db := syncdb.Open(t, "sqlite")
	repo := &knowledgeRepository{db: db}
	handled, err := repo.CleanupLegacyKnowledgeForKBDeletion(context.Background(), 1, "kb", "kid", nil)
	require.NoError(t, err)
	require.False(t, handled)
}

func TestProtectedKBDeletionUsesFreshRestrictedCapability(t *testing.T) {
	db, repo := protectedDeletionFixture(t)
	engines := []types.RetrieverEngineParams{{RetrieverEngineType: types.PostgresRetrieverEngineType}}
	ctx := context.Background()
	require.Error(t, db.Exec("DELETE FROM embeddings WHERE knowledge_id='kid'").Error,
		"ordinary SQL remains fenced")
	handled, err := repo.CleanupLegacyKnowledgeForKBDeletion(ctx, 1, "kb", "kid", engines)
	require.NoError(t, err)
	require.True(t, handled)
	for _, table := range []string{"knowledges", "chunks"} {
		var count int64
		require.NoError(t, db.Table(table).Where("deleted_at IS NULL").Count(&count).Error)
		require.Zero(t, count)
	}
	var count int64
	require.NoError(t, db.Table("embeddings").Count(&count).Error)
	require.Zero(t, count)
	var res types.FeishuLegacyResource
	require.NoError(t, db.Table("feishu_legacy_resources").Take(&res).Error)
	require.Equal(t, "revoked", res.State)
	require.EqualValues(t, 1, res.Generation)
	var attempt types.FeishuLegacyAttempt
	require.NoError(t, db.Table("feishu_legacy_attempts").Take(&attempt).Error)
	require.Equal(t, "obsolete", attempt.Status)
	require.Error(t, db.Exec("UPDATE knowledges SET channel='web' WHERE id='kid'").Error)
	// A replay is a no-op, not a new generation or another storage adjustment.
	handled, err = repo.CleanupLegacyKnowledgeForKBDeletion(ctx, 1, "kb", "kid", engines)
	require.NoError(t, err)
	require.True(t, handled)
	require.NoError(t, db.Table("feishu_legacy_attempts").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestProtectedKBDeletionRejectsUnsafeConditions(t *testing.T) {
	for _, test := range []struct{ name, change string }{
		{"live-parent", "UPDATE knowledge_bases SET deleted_at=NULL"},
		{"missing-audit", "DELETE FROM audit_logs"},
		{"foreign-tenant", "UPDATE feishu_legacy_resources SET tenant_id=2"},
		{"live-source", "UPDATE data_sources SET deleted_at=NULL"},
		{"live-execution", "INSERT INTO sync_logs(id,data_source_id,tenant_id,status,execution_claimed) " +
			"VALUES('run','ds',1,'running',true)"},
		{"live-publication", "INSERT INTO feishu_legacy_attempts(id,resource_id,generation,owner_nonce," +
			"lease_until,status,revision,source_hash,expected_hash) " +
			"VALUES('old','resource',0,'old-nonce',clock_timestamp()+interval '1 hour','staging','','','')"},
		{"stale-audit", "UPDATE audit_logs SET created_at=clock_timestamp()-interval '1 day'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, repo := protectedDeletionFixture(t)
			require.NoError(t, db.Exec(test.change).Error)
			_, err := repo.CleanupLegacyKnowledgeForKBDeletion(context.Background(), 1, "kb", "kid",
				[]types.RetrieverEngineParams{{RetrieverEngineType: types.PostgresRetrieverEngineType}})
			require.Error(t, err)
			var count int64
			require.NoError(t, db.Table("knowledges").Where("deleted_at IS NULL").Count(&count).Error)
			require.EqualValues(t, 1, count)
			require.NoError(t, db.Table("feishu_legacy_attempts").
				Where("source_hash='kb-deletion'").Count(&count).Error)
			require.Zero(t, count, "failed cleanup must not leave a deletion capability")
		})
	}
}

func TestProtectedKBDeletionRollsBackAllWrites(t *testing.T) {
	db, repo := protectedDeletionFixture(t)
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_cleanup_chunk() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'injected chunk write failure'; END $$;
 CREATE TRIGGER test_chunk_failure BEFORE UPDATE ON chunks
 FOR EACH ROW EXECUTE FUNCTION reject_cleanup_chunk()`).Error)
	_, err := repo.CleanupLegacyKnowledgeForKBDeletion(context.Background(), 1, "kb", "kid",
		[]types.RetrieverEngineParams{{RetrieverEngineType: types.PostgresRetrieverEngineType}})
	require.ErrorContains(t, err, "injected chunk write failure")
	var count int64
	require.NoError(t, db.Table("embeddings").Count(&count).Error)
	require.EqualValues(t, 1, count, "vector deletion rolls back with chunk failure")
	require.NoError(t, db.Table("feishu_legacy_attempts").Count(&count).Error)
	require.Zero(t, count, "failed transaction must leave no usable capability")
	var generation int64
	require.NoError(t, db.Table("feishu_legacy_resources").Select("generation").Scan(&generation).Error)
	require.Zero(t, generation)
}

func TestProtectedKBDeletionConcurrentReplay(t *testing.T) {
	db, repo := protectedDeletionFixture(t)
	start := make(chan struct{})
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := repo.CleanupLegacyKnowledgeForKBDeletion(context.Background(), 1, "kb", "kid",
				[]types.RetrieverEngineParams{{RetrieverEngineType: types.PostgresRetrieverEngineType}})
			errors <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var count int64
	require.NoError(t, db.Table("feishu_legacy_attempts").Count(&count).Error)
	require.EqualValues(t, 1, count)
}
