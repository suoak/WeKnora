package postgres

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestPortalLexicalNumericScopeNative(t *testing.T) {
	db, err := gorm.Open(postgres.Open(feishulegacy.PrivateDSN(t)), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	require.NoError(t, db.Exec(`CREATE TABLE knowledges(id text PRIMARY KEY,tenant_id bigint,knowledge_base_id text);
CREATE TABLE knowledge_bases(id text PRIMARY KEY,tenant_id bigint,deleted_at timestamptz);
INSERT INTO knowledge_bases VALUES('kb',1,NULL);
CREATE TABLE embeddings(id bigint PRIMARY KEY,content text,knowledge_base_id text,knowledge_id text,allowed bool);
INSERT INTO knowledges VALUES('doc',1,'kb'),('other',1,'kb'),('foreign',2,'kb');
INSERT INTO embeddings VALUES(1,'needle alpha','kb','doc',true),(2,repeat('needle ',20),'kb','doc',false),
(3,'needle','kb','other',true),(4,'needle','kb','foreign',true);
INSERT INTO embeddings SELECT n,'needle','global','outside',true FROM generate_series(10,1010)n;
CREATE INDEX portal_lexical_bm25 ON embeddings USING bm25(id,content,knowledge_base_id,knowledge_id) WITH(key_field='id')`).Error)
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
	require.Equal(t, "{1,2,3,4}", all[1], "this is only a coarse KB corpus; document and tenant authority remains in the outer relation")
	foreign, _, err := r.portalLexicalSearchScope(context.Background(), 2, "content ||| $1", []interface{}{"needle"}, params)
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
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = r.portalLexicalSearchScope(canceled, 1, "content ||| $1", []interface{}{"needle"}, params)
	require.Error(t, err, "metadata selection must obey the operation deadline")
}
