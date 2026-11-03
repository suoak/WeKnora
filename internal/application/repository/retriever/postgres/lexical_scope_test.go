package postgres

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestLexicalDocumentScopeBindings(t *testing.T) {
	unsafe := "doc' OR 1=1 --"
	where, vars := lexicalCandidateScope("content ||| $2", []interface{}{"outer", "query"}, types.RetrieveParams{
		KnowledgeBaseIDs: []string{"kb", "other-kb"}, KnowledgeIDs: []string{"doc", unsafe},
	})
	require.Equal(t, "content ||| $2 AND knowledge_base_id IN ($3,$4) AND knowledge_id IN ($5,$6)", where)
	require.Equal(t, []interface{}{"outer", "query", "kb", "other-kb", "doc", unsafe}, vars)
	require.NotContains(t, where, unsafe)
	require.NotContains(t, where, "LIMIT")
	where, vars = lexicalCandidateScope("content ||| $1", []interface{}{"query"}, types.RetrieveParams{})
	require.Equal(t, "content ||| $1", where)
	require.Equal(t, []interface{}{"query"}, vars)
}

func TestLexicalDocumentScopeAuthorizationBeforeLimitPostgres(t *testing.T) {
	db, err := gorm.Open(postgres.Open(feishulegacy.PrivateDSN(t)), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	require.NoError(t, db.Exec(`CREATE TABLE embeddings(
id int,knowledge_base_id text,knowledge_id text,score int,allowed bool)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO embeddings VALUES
(1,'kb','doc',1,true),(2,'kb','doc',1000,false),
(3,'foreign','doc',2000,true),(4,'kb','other',3000,true)`).Error)
	for _, test := range []struct {
		name   string
		params types.RetrieveParams
		count  int
	}{
		{"exact document", types.RetrieveParams{KnowledgeBaseIDs: []string{"kb"}, KnowledgeIDs: []string{"doc"}}, 2},
		{"KB only", types.RetrieveParams{KnowledgeBaseIDs: []string{"kb"}}, 3},
		{"document only", types.RetrieveParams{KnowledgeIDs: []string{"doc"}}, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			where, vars := lexicalCandidateScope("score >= $1", []interface{}{0}, test.params)
			var count int
			require.NoError(t, db.Raw("WITH lexical AS MATERIALIZED (SELECT * FROM embeddings WHERE "+
				where+") SELECT count(*) FROM lexical", vars...).Scan(&count).Error)
			require.Equal(t, test.count, count)
			// Repeat authority on the outside, as the real query does. A denied
			// higher-ranked exact document must not consume the result limit.
			query := "WITH lexical AS MATERIALIZED (SELECT * FROM embeddings WHERE " + where +
				") SELECT id FROM lexical WHERE allowed AND knowledge_base_id='kb' AND knowledge_id='doc'" +
				" ORDER BY score DESC LIMIT 1"
			var ids []int
			require.NoError(t, db.Raw(query, vars...).Scan(&ids).Error)
			require.Equal(t, []int{1}, ids)
		})
	}
}

func TestLexicalDocumentScopeNativeParadeDB(t *testing.T) {
	db, err := gorm.Open(postgres.Open(feishulegacy.PrivateDSN(t)), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	var installed bool
	require.NoError(t, db.Raw("SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='pg_search')").
		Scan(&installed).Error)
	require.True(t, installed, "this gate requires native ParadeDB, not a substitute operator")
	require.NoError(t, db.Exec(`CREATE TABLE embeddings(
id bigint PRIMARY KEY,content text,knowledge_base_id text,knowledge_id text,allowed bool)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO embeddings VALUES
(1,'needle','kb','doc',true),(2,repeat('needle ',20),'kb','doc',false),
(3,'needle','foreign','doc',true),(4,'needle','kb','other',true)`).Error)
	require.NoError(t, db.Exec(`CREATE INDEX lexical_scope_bm25 ON embeddings
USING bm25(id,content,knowledge_base_id,knowledge_id) WITH(key_field='id')`).Error)
	where, vars := lexicalCandidateScope("content ||| $1", []interface{}{"needle"}, types.RetrieveParams{
		KnowledgeBaseIDs: []string{"kb"}, KnowledgeIDs: []string{"doc"},
	})
	cte := "WITH lexical AS MATERIALIZED (SELECT embeddings.*,paradedb.score(id) AS score FROM embeddings WHERE " +
		where + ") "
	var count int
	require.NoError(t, db.Raw(cte+"SELECT count(*) FROM lexical", vars...).Scan(&count).Error)
	require.Equal(t, 2, count, "native lexical candidate materialization must honor BOTH scalar selectors")
	var ids []int
	require.NoError(t, db.Raw(cte+`SELECT id FROM lexical WHERE allowed
AND knowledge_base_id='kb' AND knowledge_id='doc' ORDER BY score DESC LIMIT 1`, vars...).Scan(&ids).Error)
	require.Equal(t, []int{1}, ids, "denied higher-ranked content must remain excluded before top-K")
}
