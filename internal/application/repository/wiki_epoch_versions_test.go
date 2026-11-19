package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWikiEpochBatchMatchesOriginalVersionsAndBoundsQueries(t *testing.T) {
	f := newEnterpriseFixture(t)
	ids := []string{"doc"}
	for i := 0; i < 31; i++ {
		id := fmt.Sprintf("wiki-batch-%02d", i)
		ids = append(ids, id)
		require.NoError(t, f.db.Exec(`INSERT INTO knowledges
(id,tenant_id,knowledge_base_id,type,parse_status,enable_status,channel,updated_at)
SELECT ?,tenant_id,knowledge_base_id,type,parse_status,enable_status,channel,updated_at
FROM knowledges WHERE id='doc'`, id).Error)
		require.NoError(t, f.db.Exec(`INSERT INTO chunks
(id,tenant_id,knowledge_base_id,knowledge_id,content,content_revision,is_enabled,chunk_type,chunk_index,updated_at)
SELECT ?,tenant_id,knowledge_base_id,?,content,content_revision,is_enabled,chunk_type,chunk_index,updated_at
FROM chunks WHERE id='chunk'`, id+"-chunk", id).Error)
	}
	require.NoError(t, f.db.Exec("DELETE FROM chunks WHERE knowledge_id=?", ids[len(ids)-1]).Error)
	chunkQueries := 0
	require.NoError(
		t,
		f.db.Callback().Query().Before("gorm:query").Register("count_wiki_epoch_chunks", func(tx *gorm.DB) {
			if tx.Statement.Table == "chunks" {
				chunkQueries++
			}
		}),
	)
	versions := newWikiEpochVersions(f.db, "public-kb", 99, ids)
	got := map[string]string{}
	for _, id := range ids {
		version, tenant, err := versions.get(id)
		require.NoError(t, err)
		require.Equal(t, uint64(99), tenant)
		got[id] = version
	}
	require.LessOrEqual(
		t,
		chunkQueries,
		4,
		"32 small documents need two bounded metadata batches, not 32 resource scans",
	)
	for _, id := range ids {
		expected, tenant, err := resourceVersion(f.db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: id})
		require.NoError(t, err)
		require.Equal(t, uint64(99), tenant)
		require.Equal(t, expected, got[id], id)
	}
	// The proof-local optimization must not depend on policy storage or cache
	// policy facts. Its caller still performs all original authorization checks.
	require.NoError(t, f.db.Exec("DROP TABLE knowledge_access_policies").Error)
	bounded := []types.EnterpriseDocumentState{{ID: ids[0]}, {ID: ids[1]}}
	require.ErrorIs(t, populateCanonicalDocumentVersions(f.db, bounded, nil, false, 1), errDocumentVersionBatchTooLarge)
	require.Empty(t, bounded[0].CurrentVersion, "an oversized fetch must not publish a partial version batch")
	_, _, err := newWikiEpochVersions(f.db, "public-kb", 99, ids).get("doc")
	require.NoError(t, err)
}

func TestWikiEpochBatchInvalidatesRawWritesAndRejectsMissingCoverage(t *testing.T) {
	f := newEnterpriseFixture(t)
	load := func() (string, error) {
		version, _, err := newWikiEpochVersions(f.db, "public-kb", 99, []string{"doc"}).get("doc")
		return version, err
	}
	old, err := load()
	require.NoError(t, err)
	require.NoError(
		t,
		f.db.Exec("UPDATE chunks SET content='changed without touching updated_at' WHERE id='chunk'").Error,
	)
	fresh, err := load()
	require.NoError(t, err)
	require.NotEqual(t, old, fresh)
	canonical, _, err := resourceVersion(f.db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: "doc"})
	require.NoError(t, err)
	require.Equal(t, canonical, fresh)
	_, _, err = newWikiEpochVersions(f.db, "public-kb", 1, []string{"doc"}).get("doc")
	require.Error(t, err, "foreign tenant must not receive a proof version")
	_, _, err = newWikiEpochVersions(f.db, "another-kb", 99, []string{"doc"}).get("doc")
	require.Error(t, err)
	_, _, err = newWikiEpochVersions(f.db, "public-kb", 99, []string{"doc"}).get("unknown")
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = newWikiEpochVersions(f.db.WithContext(ctx), "public-kb", 99, []string{"doc"}).get("doc")
	require.Error(t, err)
	warm := newWikiEpochVersions(f.db, "public-kb", 99, []string{"doc"})
	_, _, err = warm.get("doc")
	require.NoError(t, err)
	warm.db = warm.db.WithContext(ctx)
	_, _, err = warm.get("doc")
	require.Error(t, err, "a proof-local cache must still honor cancellation")
	require.NoError(t, f.db.Exec("DROP TRIGGER document_epoch_chunks_update").Error)
	_, err = load()
	require.Error(t, err, "batching must retain trigger-coverage fencing")
}

func TestWikiEpochBatchLargeDocumentUsesOriginalPath(t *testing.T) {
	f := newEnterpriseFixture(t)
	require.NoError(t, f.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<16384)
INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,content_revision,
is_enabled,chunk_type,chunk_index,updated_at)
SELECT 'large-'||i,99,'public-kb','doc','large input',1,true,'text',i,'2026-10-20 00:00:00' FROM n`).Error)
	reads := 0
	require.NoError(
		t,
		f.db.Callback().Query().Before("gorm:query").Register("large_wiki_epoch_policy", func(tx *gorm.DB) {
			if tx.Statement.Table == "document_access_epochs" {
				reads++
			}
		}),
	)
	got, tenant, err := newWikiEpochVersions(f.db, "public-kb", 99, []string{"doc"}).get("doc")
	require.NoError(t, err)
	require.Equal(t, uint64(99), tenant)
	require.NotEmpty(t, got)
	canonical, _, err := resourceVersion(f.db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: "doc"})
	require.NoError(t, err)
	require.Equal(t, canonical, got)
	require.Equal(t, 2, reads, "one original resourceVersion call plus its canonical comparison")
}
