package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestPortalLexicalOrderedIdentityPreservesAuthorizedTopKPostgres(t *testing.T) {
	db, err := gorm.Open(postgres.Open(feishulegacy.PrivateDSN(t)), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	require.NoError(t, db.Exec(`CREATE TABLE embeddings(id bigint PRIMARY KEY,content text,
 knowledge_base_id text,knowledge_id text,source_id text,chunk_id text,allowed bool);
INSERT INTO embeddings VALUES(1,'needle ordinary','kb','doc','chunk','chunk',true),
(2,'needle denied','kb','doc','denied','denied',false),
(3,'needle sibling','other-kb','other-doc','other','other',true);
INSERT INTO embeddings SELECT n,repeat('needle ',10),'kb','doc','question-'||n,'chunk',true
FROM generate_series(1000,1250)n;
CREATE INDEX portal_ordered_identity_bm25 ON embeddings
USING bm25(id,content,knowledge_base_id,knowledge_id) WITH(key_field='id')`).Error)
	r := &pgRepository{db: db}
	conditions := []clause.Expression{
		clause.Expr{SQL: "embeddings.allowed AND embeddings.source_id=embeddings.chunk_id"},
		clause.Eq{Column: "embeddings.knowledge_base_id", Value: "kb"},
	}
	params := types.RetrieveParams{Query: "needle", KnowledgeBaseIDs: []string{"kb"}, TopK: 100}
	sql, vars, err := r.portalOrderedKeywordSQL(context.Background(), params, conditions,
		[]string{"paradedb.score(id) AS score", "id", "knowledge_base_id"})
	require.NoError(t, err)
	baseline := strings.Replace(sql, " AND source_id=chunk_id ORDER BY score DESC,id)",
		" ORDER BY score DESC,id)", 1)
	require.NotEqual(t, baseline, sql)
	type scored struct {
		ID    int64
		Score float64
	}
	var before, after []scored
	for _, query := range []struct {
		sql string
		out *[]scored
	}{{baseline, &before}, {sql, &after}} {
		require.NoError(t, db.Raw("?", clause.Expr{SQL: query.sql, Vars: vars}).Scan(query.out).Error)
	}
	require.Equal(t, before, after, "candidate IDs, order and exact BM25 scores must remain identical")
	require.Len(t, after, 1, "synthetic and denied candidates cannot consume the authorized limit")
	require.Equal(t, int64(1), after[0].ID)
}

func TestPortalLexicalNumericScopeNative(t *testing.T) {
	db, err := gorm.Open(postgres.Open(feishulegacy.PrivateDSN(t)), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	require.NoError(t, db.Exec(`CREATE TABLE knowledges(id text PRIMARY KEY,tenant_id bigint,knowledge_base_id text);
CREATE TABLE knowledge_bases(id text PRIMARY KEY,tenant_id bigint,deleted_at timestamptz);
INSERT INTO knowledge_bases VALUES('kb',1,NULL),('sibling',1,NULL),('foreign-kb',2,NULL);
CREATE TABLE embeddings(id bigint PRIMARY KEY,content text,knowledge_base_id text,knowledge_id text,allowed bool);
INSERT INTO knowledges VALUES('doc',1,'kb'),('other',1,'kb'),('foreign',2,'kb');
INSERT INTO embeddings VALUES(1,'needle alpha','kb','doc',true),(2,repeat('needle ',20),'kb','doc',false),
(3,'needle','kb','other',true),(4,'needle','kb','foreign',true),
(5,'needle sibling','sibling','sibling-doc',true),(6,'needle foreign','foreign-kb','foreign-doc',true);
INSERT INTO embeddings SELECT n,'needle','global','outside',true FROM generate_series(10,1010)n;
CREATE INDEX portal_lexical_bm25 ON embeddings
USING bm25(id,content,knowledge_base_id,knowledge_id) WITH(key_field='id')`).Error)
	r := &pgRepository{db: db}
	params := types.RetrieveParams{KnowledgeBaseIDs: []string{"kb"}, KnowledgeIDs: []string{"doc"}}
	where, vars, err := r.portalLexicalSearchScope(
		context.Background(),
		1,
		"content ||| $1",
		[]interface{}{"needle"},
		params,
	)
	require.NoError(t, err)
	require.Len(t, vars, 2, "numeric sets use one bound parameter irrespective of corpus size")
	require.Equal(t, "{1,2}", vars[1])
	raw := func(query string) *gorm.DB { return db.Raw("?", clause.Expr{SQL: query, Vars: vars}) }
	cte := "WITH lexical AS MATERIALIZED (SELECT *,paradedb.score(id) score FROM embeddings WHERE " + where + ") "
	type scored struct {
		ID    int64
		Score float64
	}
	var actual, baseline []scored
	require.NoError(t, raw(cte+"SELECT id,score FROM lexical ORDER BY id").Scan(&actual).Error)
	require.NoError(
		t,
		db.Raw(`WITH lexical AS MATERIALIZED(SELECT id,paradedb.score(id) score FROM embeddings WHERE content ||| ?)
SELECT * FROM lexical WHERE id IN(1,2) ORDER BY id`, "needle").Scan(&baseline).Error,
	)
	require.Equal(t, baseline, actual, "the metadata filter must not change text relevance")
	var ids []int64
	require.NoError(
		t,
		raw(
			cte+"SELECT id FROM lexical WHERE allowed AND knowledge_id='doc' ORDER BY score DESC LIMIT 1",
		).Scan(&ids).
			Error,
	)
	require.Equal(t, []int64{1}, ids, "denied higher-ranked content cannot consume the authorized limit")
	var plan []byte
	require.NoError(t, raw("EXPLAIN(FORMAT JSON) "+cte+"SELECT count(*) FROM lexical").Row().Scan(&plan))
	require.Contains(t, string(plan), "term_set")
	require.NotContains(t, string(plan), "heap_filter")
	params.KnowledgeIDs = nil
	_, all, err := r.portalLexicalSearchScope(
		context.Background(),
		1,
		"content ||| $1",
		[]interface{}{"needle"},
		params,
	)
	require.NoError(t, err)
	require.Equal(t, "{1,2,3,4}", all[1], "coarse KB corpus; document authority remains in the outer relation")
	group := types.RetrieveParams{KnowledgeBaseIDs: []string{"kb", "sibling", "foreign-kb"}}
	groupWhere, groupVars, err := r.portalLexicalSearchScope(
		context.Background(), 1, "content ||| $1", []interface{}{"needle"}, group,
	)
	require.NoError(t, err)
	require.Equal(t, "{1,2,3,4,5}", groupVars[1], "batch IDs retain all same-tenant KBs and exclude foreign tenants")
	groupCTE := "WITH lexical AS MATERIALIZED (SELECT id,paradedb.score(id) score FROM embeddings WHERE " +
		groupWhere + ") "
	groupRaw := func(sql string) *gorm.DB { return db.Raw("?", clause.Expr{SQL: sql, Vars: groupVars}) }
	var grouped, expected []scored
	require.NoError(t, groupRaw(groupCTE+"SELECT id,score FROM lexical ORDER BY id").Scan(&grouped).Error)
	require.NoError(t, db.Raw(`WITH lexical AS MATERIALIZED(
SELECT id,paradedb.score(id) score FROM embeddings WHERE content ||| ?)
SELECT id,score FROM lexical WHERE id IN(1,2,3,4,5) ORDER BY id`, "needle").Scan(&expected).Error)
	require.Equal(t, expected, grouped, "batched numeric postings preserve text scores and the full candidate set")
	var groupPlan []byte
	require.NoError(t, groupRaw("EXPLAIN(FORMAT JSON) "+groupCTE+"SELECT count(*) FROM lexical").
		Row().Scan(&groupPlan))
	require.Contains(t, string(groupPlan), "term_set")
	require.NotContains(t, string(groupPlan), "heap_filter", "grouping must not restore a global postings scan")
	foreign, _, err := r.portalLexicalSearchScope(
		context.Background(), 2, "content ||| $1", []interface{}{"needle"}, params)
	require.NoError(t, err)
	require.Contains(t, foreign, "AND FALSE", "a different tenant cannot select this KB's index identities")
	params.KnowledgeIDs = []string{"doc' OR 1=1 --"}
	empty, _, err := r.portalLexicalSearchScope(
		context.Background(),
		1,
		"content ||| $1",
		[]interface{}{"needle"},
		params,
	)
	require.NoError(t, err)
	require.Contains(t, empty, "AND FALSE")
	params.KnowledgeIDs = nil
	require.NoError(
		t,
		db.Exec(`INSERT INTO embeddings SELECT n,'needle','kb','doc',true FROM generate_series(2000,134000)n`).Error,
	)
	fallback, _, err := r.portalLexicalSearchScope(
		context.Background(),
		1,
		"content ||| $1",
		[]interface{}{"needle"},
		params,
	)
	require.NoError(t, err)
	require.NotContains(
		t,
		fallback,
		"term_set",
		"oversized corpora retain the full original path instead of truncation",
	)
	require.Contains(t, fallback, "knowledge_base_id IN")
	group.KnowledgeBaseIDs = []string{"kb", "sibling"}
	groupFallback, fallbackVars, err := r.portalLexicalSearchScope(
		context.Background(), 1, "content ||| $1", []interface{}{"needle"}, group,
	)
	require.NoError(t, err)
	require.NotContains(t, groupFallback, "term_set")
	var fullCount int64
	require.NoError(t, db.Raw("?", clause.Expr{
		SQL:  "SELECT count(*) FROM embeddings WHERE " + groupFallback,
		Vars: fallbackVars,
	}).Scan(&fullCount).Error)
	require.Greater(t, fullCount, int64(maxPortalLexicalCandidateIDs),
		"an oversized group must never truncate candidates")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = r.portalLexicalSearchScope(canceled, 1, "content ||| $1", []interface{}{"needle"}, params)
	require.Error(t, err, "metadata selection must obey the operation deadline")
}
