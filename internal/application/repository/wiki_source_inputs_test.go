package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"gorm.io/driver/postgres"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWikiSourceInputBatchMatchesOriginalAndFallsBackCompletely(t *testing.T) {
	testWikiSourceInputBatch(t, newEnterpriseFixture(t).db)
}

func testWikiSourceInputBatch(t *testing.T, db *gorm.DB) {
	f := &enterpriseFixture{db: db}
	for _, col := range []string{
		"title", "description", "source", "file_name", "file_type",
		"file_hash", "file_path", "embedding_model_id",
	} {
		require.NoError(t, f.db.Exec("ALTER TABLE knowledges ADD COLUMN "+col+" TEXT").Error)
	}
	for _, col := range []string{"context_header", "parent_chunk_id", "image_info"} {
		require.NoError(t, f.db.Exec("ALTER TABLE chunks ADD COLUMN "+col+" TEXT").Error)
	}
	ids := []string{"doc"}
	for i := 1; i < 8; i++ {
		id := fmt.Sprintf("batch-%d", i)
		ids = append(ids, id)
		require.NoError(
			t,
			f.db.Exec(
				`INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,parse_status,
enable_status,channel,updated_at)
SELECT ?,tenant_id,knowledge_base_id,type,parse_status,enable_status,channel,updated_at
FROM knowledges WHERE id='doc'`,
				id,
			).Error,
		)
		require.NoError(
			t,
			f.db.Exec(`INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,
chunk_type,chunk_index)
VALUES(?,99,'public-kb',?,'unicode source text','text',1)`, id+"-c", id).Error,
		)
	}
	require.NoError(t, f.db.Exec("DELETE FROM chunks WHERE knowledge_id=?", ids[len(ids)-1]).Error)
	reads := 0
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("batch_source_inputs", func(tx *gorm.DB) {
		if tx.Statement.Table == "chunks" || tx.Statement.Table == "knowledges" {
			reads++
		}
	}))
	batch := newWikiSourceInputs(f.db, ids)
	got := map[string]string{}
	for _, id := range ids {
		identity, chunks, err := batch.get(id, 99, "public-kb")
		require.NoError(t, err)
		got[id] = wikiDigest([]any{identity, chunks})
	}
	require.Equal(t, 2, reads, "eight documents should need one identity and one body batch")
	for _, id := range ids {
		identity, chunks, err := batch.single(id, 99, "public-kb")
		require.NoError(t, err)
		cachedIdentity, cachedChunks, cacheErr := batch.get(id, 99, "public-kb")
		require.NoError(t, cacheErr)
		require.Equal(t, identity, cachedIdentity, id+" identity")
		require.Equal(t, chunks, cachedChunks, id+" chunks")
		require.Equal(t, wikiDigest([]any{identity, chunks}), got[id], id)
	}
	// SQL byte guard must reject a large group, then return the complete original body.
	large := strings.Repeat("\u754c", wikiSourceInputBytes/3+1)
	require.NoError(t, f.db.Exec("UPDATE chunks SET content=? WHERE knowledge_id='doc'", large).Error)
	rows, err := batch.boundedRows("chunks", wikiInputColumns, "knowledge_id IN ?", ids)
	require.NoError(t, err)
	require.Empty(t, rows)
	identity, chunks, err := newWikiSourceInputs(f.db, ids).get("doc", 99, "public-kb")
	require.NoError(t, err)
	original, originalChunks, err := batch.single("doc", 99, "public-kb")
	require.NoError(t, err)
	require.Equal(t, wikiDigest([]any{original, originalChunks}), wikiDigest([]any{identity, chunks}))
	require.NoError(t, f.db.Exec("UPDATE chunks SET content='small' WHERE knowledge_id='doc'").Error)
	_, _, err = newWikiSourceInputs(f.db, ids).get("doc", 1, "public-kb")
	require.Error(t, err)
	_, _, err = newWikiSourceInputs(f.db, ids).get("doc", 99, "other")
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = newWikiSourceInputs(f.db.WithContext(ctx), ids).get("doc", 99, "public-kb")
	require.Error(t, err)
}

func TestWikiSourceInputBatchPostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	for _, query := range []string{
		`CREATE TABLE knowledges(id text PRIMARY KEY,tenant_id bigint,knowledge_base_id text,type text,
parse_status text,enable_status text,channel text,updated_at timestamptz,deleted_at timestamptz)`,
		`CREATE TABLE chunks(id text PRIMARY KEY,tenant_id bigint,knowledge_base_id text,
knowledge_id text,content text,
content_revision bigint,is_enabled bool,chunk_type text,chunk_index integer,
updated_at timestamptz,deleted_at timestamptz)`,
		`INSERT INTO knowledges VALUES('doc',99,'public-kb','file','completed','enabled','web',now(),NULL)`,
		`INSERT INTO chunks VALUES('chunk',99,'public-kb','doc','original source',1,true,'text',0,now(),NULL)`,
	} {
		require.NoError(t, db.Exec(query).Error)
	}
	testWikiSourceInputBatch(t, db)
}
