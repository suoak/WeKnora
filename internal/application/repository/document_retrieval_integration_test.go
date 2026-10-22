//go:build sqlite_fts5

package repository

import (
	"context"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	retrieversqlite "github.com/Tencent/WeKnora/internal/application/repository/retriever/sqlite"
	"github.com/Tencent/WeKnora/internal/types"
	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/stretchr/testify/require"
)

var documentSQLiteVecOnce sync.Once

func TestDocumentM3RealSQLitePreFilterAndModelSpies(t *testing.T) {
	documentSQLiteVecOnce.Do(sqlitevec.Auto)
	f := newEnterpriseFixture(t)
	require.NoError(t, f.db.Exec(`INSERT INTO knowledges SELECT 'sensitive',tenant_id,knowledge_base_id,type,parse_status,enable_status,channel,updated_at,deleted_at FROM knowledges WHERE id='doc'`).Error)
	require.NoError(t, f.db.Exec(`INSERT INTO chunks SELECT 'secret-chunk',tenant_id,knowledge_base_id,'sensitive','ERR_M2_42 SECRET_M3_CANARY',1,true,'text',1,updated_at,deleted_at FROM chunks WHERE id='chunk'`).Error)
	f.approveClean(t)
	f.documentPolicy(t, "sensitive", "restricted")
	r := f.documentReader()
	ctx := types.WithExecutionTenant(documentCtx("A", 1), 99)
	g, err := r.Authorize(ctx, "public-kb", "doc", types.DocumentSearch)
	require.NoError(t, err)
	_, err = access.GuardDocumentRetrieval(access.WithDocumentSearch(documentCtx("A", 1), g), types.SQLiteRetrieverEngineType, types.RetrieveParams{})
	require.Error(t, err, "missing execution tenant must deny, not panic")
	_, err = access.GuardDocumentRetrieval(access.WithDocumentSearch(types.WithExecutionTenant(ctx, 100), g), types.SQLiteRetrieverEngineType, types.RetrieveParams{})
	require.Error(t, err)
	_, err = access.GuardDocumentRetrieval(access.WithDocumentSearch(ctx, g), types.PostgresRetrieverEngineType, types.RetrieveParams{})
	require.Error(t, err, "uncertified driver blocked before invocation")
	tooMany := make([]*access.DocumentReadGrant, access.MaxDocumentAllowSet+1)
	for i := range tooMany {
		tooMany[i] = g
	}
	_, err = access.GuardDocumentRetrieval(access.WithDocumentSearch(ctx, tooMany...), types.SQLiteRetrieverEngineType, types.RetrieveParams{})
	require.Error(t, err)
	engine := retrieversqlite.NewSQLiteRetrieveEngineRepository(f.db)
	for _, row := range []struct {
		id, doc, body string
		vector        []float32
	}{
		{"chunk", "doc", "Synthetic approved knowledge: ERR_M2_42", []float32{0.8, 0.2, 0}},
		{"secret-chunk", "sensitive", "ERR_M2_42 SECRET_M3_CANARY", []float32{1, 0, 0}},
		{"stale-index", "doc", "ERR_M2_42 STALE_SECRET_CANARY", []float32{1, 0, 0}},
	} {
		require.NoError(t, engine.Save(ctx, &types.IndexInfo{SourceID: row.id, ChunkID: row.id, KnowledgeID: row.doc, KnowledgeBaseID: "public-kb", Content: row.body, SourceType: types.ChunkSourceType, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{row.id: row.vector}}))
	}
	for _, mode := range []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType, ""} {
		calls := 0
		spy := func(_ context.Context, rows []*types.IndexWithScore) error {
			calls++
			require.NotEmpty(t, rows)
			for _, row := range rows {
				require.Equal(t, "doc", row.KnowledgeID)
				require.NotContains(t, row.Content, "CANARY")
			}
			return nil
		}
		params := types.RetrieveParams{Query: "ERR_M2_42", Embedding: []float32{1, 0, 0}, RetrieverType: mode, TopK: 1}
		rows, err := access.SearchDocuments(ctx, []*access.DocumentReadGrant{g}, engine, params, spy, spy)
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		require.Equal(t, 2, calls)
		_, err = access.SearchDocuments(ctx, nil, engine, params, spy)
		require.Error(t, err)
		require.Equal(t, 2, calls)
		params.KnowledgeIDs = []string{"sensitive"}
		_, err = access.SearchDocuments(ctx, []*access.DocumentReadGrant{g}, engine, params, spy)
		require.Error(t, err)
		require.Equal(t, 2, calls)
	}
	// Revoke after retrieval, between Rerank and LLM. The LLM spy must not run.
	llmCalls := 0
	_, err = access.SearchDocuments(ctx, []*access.DocumentReadGrant{g}, engine, types.RetrieveParams{Query: "ERR_M2_42", RetrieverType: types.KeywordsRetrieverType, TopK: 5}, func(context.Context, []*types.IndexWithScore) error {
		return f.db.Exec(`UPDATE tenant_portal_configs SET status='archived' WHERE tenant_id=99`).Error
	}, func(context.Context, []*types.IndexWithScore) error { llmCalls++; return nil })
	require.Error(t, err)
	require.Zero(t, llmCalls)
}
