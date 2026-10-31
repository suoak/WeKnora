package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestExactPublishedChunkPageBoundariesAndPagination(t *testing.T) {
	db := setupChunkTestDB(t)
	require.NoError(t, db.Exec("CREATE TABLE knowledges(id text, knowledge_base_id text)").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledges VALUES ('doc','kb')").Error)
	require.NoError(t, db.Exec("CREATE TABLE task_pending_ops(scope text,scope_id text)").Error)
	require.NoError(
		t,
		db.Exec("INSERT INTO task_pending_ops VALUES (?,?)", types.PersonalExactReceiptScope, "doc").Error,
	)
	for i := range 63 {
		c := makeChunk("kb", "doc", types.ChunkTypeText)
		c.ChunkIndex = i
		c.Content = fmt.Sprintf("published paragraph %d", i)
		if i == 62 {
			c.ChunkType = types.ChunkTypeSummary
		}
		require.NoError(t, db.Create(c).Error)
	}
	db = types.EnableLegacyReadGuards(db)
	repo := NewChunkRepository(db)
	ctx := types.WithExecutionTenant(context.Background(), 1)
	ctx = context.WithValue(ctx, types.PrincipalContextKey, types.Principal{Type: types.PrincipalWebUser, ID: "viewer"})
	calls := 0
	revokeOnReturn := false
	ctx, err := types.WithExactPublishedDocumentRead(ctx, 1, types.ExactDocumentScope{
		TenantID: 1, KnowledgeID: "doc", KBID: "kb", Read: func(context.Context) error {
			calls++
			if revokeOnReturn && calls%2 == 1 {
				return types.ErrExactDocumentScope
			}
			return nil
		},
	})
	require.NoError(t, err)
	for i, expected := range []int{25, 25, 12} {
		rows, total, err := repo.ListPagedChunksByKnowledgeID(
			ctx,
			1,
			"doc",
			&types.Pagination{
				Page:     i + 1,
				PageSize: 25,
			},
			[]types.ChunkType{types.ChunkTypeText},
			nil,
			"",
			"",
			"",
			"",
			nil,
		)
		require.NoError(t, err)
		require.EqualValues(t, 62, total)
		require.Len(t, rows, expected)
		for n, row := range rows {
			require.Equal(t, (i*25)+n, row.ChunkIndex)
		}
	}
	require.Equal(t, 7, calls, "one issue plus two live checks per page")
	rows, _, err := repo.ListPagedChunksByKnowledgeID(ctx, 2, "doc",
		&types.Pagination{Page: 1, PageSize: 25}, []types.ChunkType{types.ChunkTypeText}, nil, "", "", "", "", nil)
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	require.Nil(t, rows)
	rows, _, err = repo.ListPagedChunksByKnowledgeID(types.WithExecutionTenant(context.Background(), 1), 1, "doc",
		&types.Pagination{Page: 1, PageSize: 25}, []types.ChunkType{types.ChunkTypeText}, nil, "", "", "", "", nil)
	require.ErrorIs(
		t,
		err,
		types.ErrLegacySourceRestricted,
		"receipt without current viewer proof is not serving permission",
	)
	require.Nil(t, rows)
	revokeOnReturn = true
	rows, total, err := repo.ListPagedChunksByKnowledgeID(ctx, 1, "doc",
		&types.Pagination{Page: 1, PageSize: 25}, []types.ChunkType{types.ChunkTypeText}, nil, "", "", "", "", nil)
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	require.Nil(t, rows, "discard selected content if the final source proof fails")
	require.Zero(t, total)
}
