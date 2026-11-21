package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPortalSQLDimensionAndCanonicalEquivalencePostgres(t *testing.T) {
	db, err := gorm.Open(postgres.Open(feishulegacy.PrivateDSN(t)), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	for _, sql := range []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		`CREATE TABLE tenants(id bigint PRIMARY KEY,status text,deleted_at timestamp)`,
		`CREATE TABLE knowledge_bases(id text PRIMARY KEY,tenant_id bigint,is_temporary bool,deleted_at timestamp)`,
		`CREATE TABLE knowledges(id text PRIMARY KEY,tenant_id bigint,knowledge_base_id text,
 title text,type text,parse_status text,enable_status text,deleted_at timestamp)`,
		`CREATE TABLE chunks(id text PRIMARY KEY,tenant_id bigint,knowledge_id text,knowledge_base_id text,
 content text,context_header text,is_enabled bool,chunk_type text,deleted_at timestamp)`,
		`CREATE TABLE embeddings(id bigint PRIMARY KEY,source_id text,source_type int,chunk_id text,
 knowledge_id text,knowledge_base_id text,content text,dimension int,is_enabled bool,embedding halfvec)`,
		`CREATE TABLE knowledge_access_policies(knowledge_id text,state text,policy text,
 content_class text,source_acl_state text)`,
		`CREATE TABLE kb_publication_policies(knowledge_base_id text,state text,policy text,
 content_class text,source_acl_state text)`,
		`CREATE TABLE enterprise_space_policies(tenant_id bigint,state text,policy text,
 content_class text,source_acl_state text)`,
		`CREATE TABLE tenant_portal_configs(tenant_id bigint,status text)`,
		`CREATE TABLE task_pending_ops(scope text,scope_id text)`,
		`CREATE TABLE feishu_legacy_resources(knowledge_base_id text,state text)`,
		`CREATE TABLE data_sources(knowledge_base_id text,legacy_access_state text)`,
		`CREATE TABLE feishu_private_kbs(kb_id text,owner_id text)`,
		`INSERT INTO tenants VALUES(1,'active',NULL),(2,'active',NULL)`,
		`INSERT INTO knowledge_bases VALUES('kb',1,false,NULL),('foreign',2,false,NULL)`,
		`INSERT INTO tenant_portal_configs VALUES(1,'published'),(2,'draft')`,
		`INSERT INTO knowledges VALUES('doc',1,'kb','title','file','completed','enabled',NULL)`,
		`INSERT INTO chunks VALUES('chunk',1,'doc','kb','ordinary text','',true,'text',NULL)`,
		`ALTER TABLE knowledges ADD COLUMN channel text DEFAULT 'web'`,
	} {
		require.NoError(t, db.Exec(sql).Error)
	}
	old := portalCanonicalPredicate("?", false, true)
	newPredicate := portalDimensionCanonicalPredicate("?", false, true)
	require.Contains(t, newPredicate, " OFFSET 0)")
	// Incompatible rows deliberately include NULL and invalid dimensions.
	for _, dimensions := range [][]any{{1024}, {1024, 798}, {1024, 3584}, {nil}, {0}, {-1}, {}} {
		require.NoError(t, db.Exec("DELETE FROM embeddings").Error)
		for i, dimension := range dimensions {
			require.NoError(t, db.Exec(`INSERT INTO embeddings VALUES(?,? ,?,?,'doc','kb','ordinary text',?,true,NULL)`,
				i+1, "chunk", types.ChunkSourceType, "chunk", dimension).Error)
		}
		var counts struct{ Total, Matching int64 }
		require.NoError(t, db.Table("embeddings").Select(
			"COUNT(*) AS total, COUNT(*) FILTER(WHERE dimension=1024) AS matching",
		).Where(old, 1, types.ChunkSourceType).Scan(&counts).Error)
		var shape struct{ Incompatible, Matching bool }
		relation := func() *gorm.DB {
			return db.Table("embeddings").Select("1").Where(newPredicate, 1, types.ChunkSourceType)
		}
		require.NoError(t, db.Raw("SELECT EXISTS (?) AS incompatible, EXISTS (?) AS matching",
			relation().Where("dimension IS DISTINCT FROM ?", 1024),
			relation().Where("dimension = ?", 1024)).Scan(&shape).Error)
		require.Equal(t, counts.Total != counts.Matching, shape.Incompatible, fmt.Sprint(dimensions))
		require.Equal(t, counts.Matching > 0, shape.Matching)
	}
	// Compare protected canonical resource IDs and exact vector rank before and
	// after planner fencing, including private and Published-public predicates.
	require.NoError(t, db.Exec(`INSERT INTO embeddings VALUES
 (10,'chunk',?,'chunk','doc','kb','ordinary text',1024,true,'[1,0,0]'),
 (11,'chunk',?,'chunk','doc','foreign','ordinary text',1024,true,'[1,0,0]'),
 (12,'chunk',?,'chunk','doc','kb','STALE CANARY',1024,true,'[1,0,0]')`,
		types.ChunkSourceType, types.ChunkSourceType, types.ChunkSourceType).Error)
	require.NoError(t, db.Exec(`INSERT INTO chunks VALUES
 ('chunk2',1,'doc','kb','second current text','',true,'text',NULL),
 ('chunk3',1,'doc','kb','third current text','',true,'text',NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO embeddings VALUES
 (13,'chunk2',?,'chunk2','doc','kb','second current text',1024,true,'[0,1,0]'),
 (14,'chunk3',?,'chunk3','doc','kb','third current text',1024,true,'[0.8,0.2,0]')`,
		types.ChunkSourceType, types.ChunkSourceType).Error)
	require.NoError(t, db.Exec(`ALTER TABLE embeddings ADD COLUMN tag_id text`).Error)
	t.Run("unbounded_candidates_and_malformed_untrusted_vectors", func(t *testing.T) {
		// More stale nearest neighbours than the old expanded candidate budget
		// must not hide a farther canonical result. No pre-proof LIMIT is safe.
		require.NoError(t, db.Exec(`INSERT INTO embeddings
 (id,source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,content,dimension,is_enabled,embedding)
 SELECT n,'chunk2',?,'chunk2','doc','kb','STALE NEAREST CANARY',1024,true,'[1,0,0]'
 FROM generate_series(1000,1220) n`, types.ChunkSourceType).Error)
		require.NoError(t, db.Exec(`INSERT INTO embeddings
 (id,source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,content,dimension,is_enabled,embedding)
 VALUES(9998,'chunk',?,'chunk','doc','kb','ordinary text',1024,true,NULL),
 (9999,'chunk',?,'chunk','doc','kb','MALFORMED STALE CANARY',1024,true,'[1,0]')`,
			types.ChunkSourceType, types.ChunkSourceType).Error)
		defer func() { require.NoError(t, db.Exec("DELETE FROM embeddings WHERE id>=1000").Error) }()
		for _, public := range []bool{false, true} {
			assertPortalVectorEquivalent(t, db, public, 2, 3, "stale rows exceed candidate budget")
		}
	})
	t.Run("malformed_canonical_vector_fails_closed", func(t *testing.T) {
		require.NoError(t, db.Exec(`INSERT INTO embeddings
 (id,source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,content,dimension,is_enabled,embedding)
 VALUES(9999,'chunk',?,'chunk','doc','kb','ordinary text',1024,true,'[1,0]')`,
			types.ChunkSourceType).Error)
		defer func() { require.NoError(t, db.Exec("DELETE FROM embeddings WHERE id=9999").Error) }()
		for _, public := range []bool{false, true} {
			for _, sql := range portalVectorEquivalenceQueries(public) {
				var rows []pgVectorWithScore
				err := db.Raw(sql, "[1,0,0]", 1024, 1, types.ChunkSourceType, "kb", "viewer", 200, 2, 1).
					Scan(&rows).Error
				require.Error(t, err, "authorized malformed vectors must not become a successful omission")
			}
		}
	})
	for _, mutation := range []string{
		"SELECT 1",
		`INSERT INTO feishu_legacy_resources VALUES(NULL,'hold'),('foreign','revoked')`,
		`INSERT INTO feishu_legacy_resources VALUES('kb','hold')`,
		`DELETE FROM feishu_legacy_resources`,
		`INSERT INTO data_sources VALUES('kb','revoked')`,
		`DELETE FROM data_sources`,
		`INSERT INTO task_pending_ops VALUES('personal_exact_receipt_v1','doc')`,
		`DELETE FROM task_pending_ops`,
		`INSERT INTO task_pending_ops VALUES('personal_exact_receipt_v1',NULL),('unrelated','doc')`,
		`INSERT INTO data_sources VALUES(NULL,'hold')`,
		`INSERT INTO feishu_private_kbs VALUES(NULL,'other'),('kb','viewer')`,
		`UPDATE knowledges SET channel='feishu_personal' WHERE id='doc'`,
		`UPDATE knowledges SET channel='web' WHERE id='doc'`,
		`INSERT INTO feishu_private_kbs VALUES('kb','other')`,
		`DELETE FROM feishu_private_kbs`,
		"UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=1",
		"UPDATE tenant_portal_configs SET status='published' WHERE tenant_id=1",
		"UPDATE chunks SET is_enabled=false",
		"UPDATE chunks SET is_enabled=true; UPDATE knowledges SET deleted_at=now()",
		"UPDATE knowledges SET deleted_at=NULL; UPDATE tenants SET status='inactive' WHERE id=1",
		"UPDATE tenants SET status='active' WHERE id=1",
		`INSERT INTO knowledge_access_policies VALUES('doc','approved','deny','ordinary','local')`,
		`UPDATE knowledge_access_policies SET policy='inherit',source_acl_state='revoked'`,
		`UPDATE knowledge_access_policies SET source_acl_state='local',state='revoked'`,
		`DELETE FROM knowledge_access_policies`,
		`UPDATE chunks SET deleted_at=now()`,
		`DELETE FROM chunks`,
		`INSERT INTO chunks VALUES('chunk',1,'doc','kb','new version','',true,'text',NULL)`,
	} {
		require.NoError(t, db.Exec(mutation).Error)
		var before, after []int
		require.NoError(t, db.Table("embeddings").Select("id").Where(old, 1, types.ChunkSourceType).
			Order("id").Scan(&before).Error)
		require.NoError(t, db.Table("embeddings").Select("id").Where(newPredicate, 1, types.ChunkSourceType).
			Order("id").Scan(&after).Error)
		require.Equal(t, before, after, mutation)
		for _, public := range []bool{false, true} {
			candidate := portalDimensionCanonicalPredicate("?", public, true)
			baseline := strings.TrimSuffix(candidate, " OFFSET 0)") + ")"
			type ranked struct {
				ID       int
				Distance float64
			}
			var oldRank, newRank []ranked
			for _, query := range []struct {
				predicate string
				out       *[]ranked
			}{
				{baseline, &oldRank}, {candidate, &newRank},
			} {
				require.NoError(t, db.Table("embeddings").
					Select("id, embedding <=> '[1,0,0]'::halfvec AS distance").
					Where(query.predicate, 1, types.ChunkSourceType).
					Where("knowledge_base_id IN ?", []string{"kb"}).
					Order("distance ASC, id ASC").Limit(3).Scan(query.out).Error)
			}
			require.Equal(t, oldRank, newRank, "public=%v mutation=%s", public, mutation)
			for _, limit := range []int{1, 3} {
				for _, threshold := range []float64{0.1, 2} {
					assertPortalVectorEquivalent(t, db, public, threshold, limit, mutation)
				}
			}
		}
		for _, id := range after {
			require.Contains(t, []int{10, 13, 14}, id, "stale/cross-KB row must not be authorized")
		}
	}
}

func assertPortalVectorEquivalent(t *testing.T, db *gorm.DB,
	public bool, threshold float64, limit int, mutation string,
) {
	t.Helper()
	queries := portalVectorEquivalenceQueries(public)
	args := []any{"[1,0,0]", 1024, 1, types.ChunkSourceType, "kb", "viewer", 200, threshold, limit}
	var before, after []pgVectorWithScore
	for _, query := range []struct {
		sql string
		out *[]pgVectorWithScore
	}{{queries[0], &before}, {queries[1], &after}} {
		require.NoError(t, db.Raw(query.sql, args...).Scan(query.out).Error)
	}
	require.Equal(t, before, after, "public=%v limit=%d threshold=%v mutation=%s", public, limit, threshold, mutation)
}

func portalVectorEquivalenceQueries(public bool) [2]string {
	const base = `WHERE embeddings.dimension=$2 AND embeddings.knowledge_base_id IN ($5)
 AND (embeddings.is_enabled IS NULL OR embeddings.is_enabled=true)`
	privateOld := privateSourceCandidatePredicate("$6")
	privateNew := portalExcludedSet("embeddings.knowledge_base_id",
		"SELECT kb_id FROM feishu_private_kbs WHERE kb_id IS NOT NULL AND owner_id <> $6")
	oldWhere := base + " AND " + portalCanonicalPredicate("$3", public, true) +
		" AND " + legacySourceCandidatePredicate(context.Background()) + " AND " + privateOld
	newWhere := base + " AND " + portalOrdinarySourcePredicate() + " AND " + privateNew
	beforeSQL := fmt.Sprintf(`WITH authorized AS MATERIALIZED (
 SELECT id,%s AS content,source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,tag_id,embedding
 FROM embeddings %s)
 SELECT id,content,source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,tag_id,
 1-(embedding::halfvec(3) <=> $1::halfvec(3)) AS score
 FROM authorized WHERE $7::int>0 AND (embedding::halfvec(3) <=> $1::halfvec(3)) <= $8
 ORDER BY (embedding::halfvec(3) <=> $1::halfvec(3)),id LIMIT $9`, portalCanonicalTextProjection(), oldWhere)
	afterSQL := portalVectorSQL(3, "$3", "acl_c.knowledge_base_id IN ($5)", newWhere, public, true, 7, 8, 9)
	return [2]string{beforeSQL, afterSQL}
}
