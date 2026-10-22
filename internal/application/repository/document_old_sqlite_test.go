//go:build sqlite_fts5 && knowhub_m3_old_sqlite

package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	retrieversqlite "github.com/Tencent/WeKnora/internal/application/repository/retriever/sqlite"
	"github.com/Tencent/WeKnora/internal/types"
	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/stretchr/testify/require"
)

// Explicit negative build: compile sqlite-vec against the test host's 3.31
// headers, which omit rowid-IN handling even with a newer runtime SQLite.
func TestDocumentM3OldSQLiteBuildHOLD(t *testing.T) {
	documentSQLiteVecOnce.Do(sqlitevec.Auto)
	f := newEnterpriseFixture(t)
	f.approveClean(t)
	ctx := types.WithExecutionTenant(documentCtx("A", 1), 99)
	g, err := f.documentReader().Authorize(ctx, "public-kb", "doc", types.DocumentSearch)
	require.NoError(t, err)
	engine := retrieversqlite.NewSQLiteRetrieveEngineRepository(f.db)
	require.NoError(t, engine.Save(ctx, &types.IndexInfo{SourceID: "chunk", ChunkID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "public-kb", SourceType: types.ChunkSourceType, Content: "Synthetic approved knowledge: ERR_M2_42", IsEnabled: true}, map[string]any{"embedding": map[string][]float32{"chunk": []float32{1, 0, 0}}}))
	calls := 0
	rows, err := access.SearchDocuments(ctx, []*access.DocumentReadGrant{g}, engine, types.RetrieveParams{Embedding: []float32{1, 0, 0}, RetrieverType: types.VectorRetrieverType, TopK: 1}, func(_ context.Context, _ []*types.IndexWithScore) error { calls++; return nil })
	require.ErrorContains(t, err, "planner capability unavailable: HOLD")
	require.Empty(t, rows)
	require.Zero(t, calls)
}
