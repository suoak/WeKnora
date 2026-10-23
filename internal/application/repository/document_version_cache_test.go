package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDocumentVersionCacheKeepsCanonicalHashAndInvalidatesRawWrites(t *testing.T) {
	f := newEnterpriseFixture(t)
	cache := newDocumentVersionCache(2)
	chunkQueries := 0
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("count_version_chunks", func(tx *gorm.DB) {
		if tx.Statement.Table == "chunks" {
			chunkQueries++
		}
	}))
	load := func() string {
		var docs []types.EnterpriseDocumentState
		require.NoError(t, f.db.Table("knowledges").
			Select("id,tenant_id,knowledge_base_id,type,parse_status,enable_status,channel").
			Where("deleted_at IS NULL").Find(&docs).Error)
		require.Len(t, docs, 1)
		require.NoError(t, populateDocumentVersionsCached(f.db, docs, cache))
		return docs[0].CurrentVersion
	}
	canonical := func(version string) {
		legacy, _, err := resourceVersion(f.db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: "doc"})
		require.NoError(t, err)
		require.Equal(t, legacy, version)
	}
	previous := load()
	require.Equal(t, 1, chunkQueries)
	canonical(previous)
	chunkQueries = 0
	require.Equal(t, previous, load())
	require.Zero(t, chunkQueries, "unchanged epochs must avoid scanning chunk metadata")
	for _, mutation := range []string{
		"UPDATE chunks SET content='raw SQL changed text' WHERE id='chunk'",
		"UPDATE chunks SET is_enabled=false WHERE id='chunk'",
		"UPDATE chunks SET deleted_at='2026-10-23 00:00:00' WHERE id='chunk'",
		"DELETE FROM chunks WHERE id='chunk'",
		"UPDATE knowledges SET channel='api' WHERE id='doc'",
		"INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content" +
			",content_revision,is_enabled,chunk_type,chunk_index,updated_at) VALUES" +
			"('new-chunk',99,'public-kb','doc','new text',1,true,'text',0,'2026-10-" +
			"20 00:00:00')",
	} {
		require.NoError(t, f.db.Exec(mutation).Error)
		chunkQueries = 0
		version := load()
		require.NotEqual(t, previous, version, mutation)
		require.Equal(t, 1, chunkQueries, "fresh epoch must recompute the canonical version")
		canonical(version)
		previous = version
	}

	require.NoError(t, f.db.Exec("DELETE FROM chunks").Error)
	require.NoError(t, f.db.Exec("DELETE FROM knowledges WHERE id='doc'").Error)
	require.NoError(t, f.db.Exec("INSERT INTO knowledges VALUES('doc',99,'public-kb','file','completed',"+
		"'enabled','web','2026-10-20 00:00:00',NULL)").Error)
	recreated := load()
	require.NotEqual(t, previous, recreated, "recreating an ID must invalidate its cached version")
	canonical(recreated)
	require.NoError(t, f.db.Exec("DROP TRIGGER document_epoch_chunks_update").Error)
	var docs []types.EnterpriseDocumentState
	require.NoError(t, f.db.Table("knowledges").Find(&docs).Error)
	require.Error(t, populateDocumentVersionsCached(f.db, docs, cache),
		"warm cache must not bypass missing trigger coverage",
	)
}

func TestDocumentVersionCacheDoesNotCacheAuthorizationFacts(t *testing.T) {
	f := newEnterpriseFixture(t)
	load := func() *types.EnterpriseReadSnapshot {
		snapshot, err := f.repo.LoadDocumentReadSnapshot(context.Background(), "A", 1, "public-kb")
		require.NoError(t, err)
		return snapshot
	}
	before := load()
	require.True(t, before.PortalPublished)
	require.NoError(t, f.db.Exec("UPDATE users SET is_active=false WHERE id='A'").Error)
	require.NoError(t, f.db.Exec("UPDATE tenant_api_keys SET revoked_at='2026-10-23 00:00:00' WHERE id=1").Error)
	require.NoError(t, f.db.Exec("UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=99").Error)
	require.NoError(t, f.db.Exec("INSERT INTO knowledge_access_policies(knowledge_id,state,policy,conten"+
		"t_class,source_acl_state,revision,updated_by) VALUES('doc','pending_re"+
		"view','deny','ordinary','local',1,'owner')").Error)
	after := load()
	require.Equal(t, before.Documents[0].CurrentVersion, after.Documents[0].CurrentVersion,
		"content version remains reusable",
	)
	require.False(t, after.UserActive)
	require.False(t, after.PortalPublished)
	require.NotNil(t, after.Credential.RevokedAt)
	require.Equal(t, "deny", after.Documents[0].Policy)
}

func TestDocumentVersionCacheConcurrentBoundedAndExact(t *testing.T) {
	cache := newDocumentVersionCache(3)
	stamp := documentVersionStamp{ID: "doc", KBID: "kb", TenantID: 1, Generation: 1, Incarnation: "incarnation", UpdatedAt: time.Now()}
	cache.put(stamp, "original")
	changed := stamp
	changed.Generation++
	_, hit := cache.get(changed)
	require.False(t, hit)
	changed = stamp
	changed.Incarnation = "recreated"
	_, hit = cache.get(changed)
	require.False(t, hit)
	changed = stamp
	changed.TenantID++
	_, hit = cache.get(changed)
	require.False(t, hit)
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				key := stamp
				key.ID = fmt.Sprint(i % 4)
				key.Generation = int64(j)
				value := fmt.Sprint(j)
				cache.put(key, value)
				if actual, ok := cache.get(key); ok && actual != value {
					t.Errorf("cache returned a different generation's fingerprint")
				}
			}
		}(i)
	}
	workers.Wait()
	require.LessOrEqual(t, len(cache.entries), 3)
}
