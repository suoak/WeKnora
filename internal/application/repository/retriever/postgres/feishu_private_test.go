package postgres

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFeishuV2PrivateCandidatePredicateSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	privateCandidateSuite(t, db)
}

func TestFeishuV2PrivateCandidatePredicatePostgres(t *testing.T) {
	db, err := gorm.Open(postgres.Open(feishulegacy.PrivateDSN(t)), &gorm.Config{})
	require.NoError(t, err)
	privateCandidateSuite(t, db)
}

func privateCandidateSuite(t *testing.T, db *gorm.DB) {
	t.Helper()
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	for _, ddl := range []string{
		"CREATE TABLE embeddings(knowledge_base_id TEXT, score INTEGER)",
		"CREATE TABLE feishu_private_kbs(kb_id TEXT, owner_id TEXT)",
		"INSERT INTO embeddings VALUES('private',100),('public',1)",
		"INSERT INTO feishu_private_kbs VALUES('private','A')",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	for _, user := range []string{"A", "B", "system-1", "", "A' OR '1'='1"} {
		ctx := context.WithValue(context.Background(), types.UserIDContextKey, user)
		for _, relation := range []string{
			"SELECT knowledge_base_id FROM embeddings",
			"WITH lexical AS MATERIALIZED (SELECT * FROM embeddings) " +
				"SELECT knowledge_base_id FROM lexical AS embeddings",
		} {
			var kb string
			query := relation + " WHERE " + privateSourceCandidatePredicate("?") + " ORDER BY score DESC LIMIT 1"
			require.NoError(t, db.Raw(query, types.PrivateKBReadUser(ctx)).Scan(&kb).Error)
			if user == "A" {
				require.Equal(t, "private", kb)
			} else {
				require.Equal(t, "public", kb, "private candidates must be removed before top-K ranking")
			}
		}
	}
}
