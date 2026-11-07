package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestExactPublishedLexicalNativeCandidateScope(t *testing.T) {
	db, err := gorm.Open(postgres.Open(feishulegacy.PrivateDSN(t)), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	require.NoError(t, db.Exec(`CREATE TABLE embeddings(
id bigint PRIMARY KEY,content text,knowledge_base_id text,knowledge_id text,allowed bool)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO embeddings VALUES
(1,'needle alpha','kb','doc',true),(2,repeat('needle ',20),'kb','doc',false),
(3,'needle','foreign','doc',true),(4,'needle','kb','other',true);
INSERT INTO embeddings SELECT n,'needle','foreign','unrelated',true FROM generate_series(10,1010)n`).Error)
	require.NoError(t, db.Exec(`CREATE INDEX exact_lexical_bm25 ON embeddings
USING bm25(id,content,knowledge_base_id,knowledge_id) WITH(key_field='id')`).Error)
	checks := 0
	revoked := false
	ctx := types.WithExecutionTenant(context.Background(), 1)
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "reader"})
	ctx, err = types.WithExactPublishedDocumentRead(ctx, 7, types.ExactDocumentScope{
		TenantID: 1, KnowledgeID: "doc", KBID: "kb",
		Read: func(context.Context) error {
			checks++
			if revoked {
				return types.ErrExactDocumentScope
			}
			return nil
		},
	})
	require.NoError(t, err)
	repo := &pgRepository{db: db}
	params := types.RetrieveParams{KnowledgeBaseIDs: []string{"kb"}, KnowledgeIDs: []string{"doc"}}
	where, vars, err := repo.lexicalSearchScope(ctx, "content ||| $1", []interface{}{"needle"}, params)
	require.NoError(t, err)
	require.Equal(t, "content ||| $1 AND id @@@ paradedb.boost(0::real,"+
		"paradedb.term_set('id',ARRAY[$2::bigint,$3::bigint]::bigint[]))", where)
	require.Equal(t, []interface{}{"needle", int64(1), int64(2)}, vars)
	require.Equal(t, 3, checks, "issuance, before metadata selection, and before lexical query")
	cte := "WITH lexical AS MATERIALIZED (SELECT embeddings.*,paradedb.score(id) AS score FROM embeddings WHERE " +
		where + ") "
	var ids []int64
	raw := func(query string, args ...interface{}) *gorm.DB {
		return db.Raw("?", clause.Expr{SQL: query, Vars: args})
	}
	require.NoError(t, raw(cte+`SELECT id FROM lexical WHERE allowed
AND knowledge_base_id='kb' AND knowledge_id='doc' ORDER BY score DESC,id LIMIT 1`, vars...).Scan(&ids).Error)
	require.Equal(t, []int64{1}, ids, "the denied candidate cannot consume the authorized limit")
	type scoredRow struct {
		ID    int64
		Score float64
	}
	var scopedScores, textScores []scoredRow
	require.NoError(t, raw(cte+"SELECT id,score FROM lexical ORDER BY id", vars...).Scan(&scopedScores).Error)
	require.NoError(t, db.Raw("WITH all_matches AS MATERIALIZED "+
		"(SELECT id,paradedb.score(id) score FROM embeddings WHERE content ||| ?) "+
		"SELECT id,score FROM all_matches WHERE id IN (1,2) ORDER BY id", "needle").Scan(&textScores).Error)
	require.Equal(t, textScores, scopedScores, "numeric metadata must not boost or otherwise change text relevance")
	var plan []byte
	require.NoError(t, raw("EXPLAIN(FORMAT JSON) "+cte+"SELECT count(*) FROM lexical", vars...).Row().Scan(&plan))
	var decoded []map[string]interface{}
	require.NoError(t, json.Unmarshal(plan, &decoded))
	encoded, err := json.Marshal(decoded)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "term_set", "the indexed numeric identity must constrain native candidates")
	require.NotContains(t, string(encoded), "heap_filter", "no global lexical scan followed by UUID heap filters")
	params.KnowledgeIDs = []string{"other"}
	_, _, err = repo.lexicalSearchScope(ctx, "content ||| $1", []interface{}{"needle"}, params)
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	revoked = true
	params.KnowledgeIDs = []string{"doc"}
	_, _, err = repo.lexicalSearchScope(ctx, "content ||| $1", []interface{}{"needle"}, params)
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	where, vars, err = repo.lexicalSearchScope(context.Background(), "content ||| $1", []interface{}{"needle"}, params)
	require.NoError(t, err)
	require.Contains(t, where, "knowledge_base_id IN")
	require.Contains(t, where, "knowledge_id IN")
	require.Equal(t, []interface{}{"needle", "kb", "doc"}, vars, "ordinary retrieval is unchanged")
}
