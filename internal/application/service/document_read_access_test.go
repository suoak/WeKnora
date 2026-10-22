package service

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDocumentM5ActualKnowledgeChunkServices(t *testing.T) {
	f := fixture.New(t)
	policy := NewDocumentReadPolicy(true, f.Reader)
	knowledge := &knowledgeService{repo: repository.NewKnowledgeRepository(f.DB), documentACL: policy}
	chunks := &chunkService{chunkRepository: repository.NewChunkRepository(f.DB), knowledgeRepo: knowledge.repo, documentACL: policy}
	a := fixture.Key("A", 1, 1, true)
	d := fixture.Key("D", 99, 4, false)
	c := fixture.Key("C", 99, 2, false)
	row, err := knowledge.GetKnowledgeByID(a, "public-doc")
	require.NoError(t, err)
	require.Equal(t, fixture.PublicText, row.Title)
	require.Empty(t, row.FilePath)
	require.Empty(t, row.Metadata)
	require.Empty(t, row.Description)
	caller := types.CallerFromContext(a)
	require.Equal(t, uint64(1), caller.TenantID)
	_, err = access.ResolveKB(a, access.KBRequest{Caller: caller}, &types.KnowledgeBase{ID: "public-kb", TenantID: 99}, types.OrgRoleAdmin, nil, nil)
	require.Error(t, err)
	for _, doc := range []string{"denied-doc", "sensitive", "unknown-doc"} {
		_, err = knowledge.GetKnowledgeByID(a, doc)
		require.Error(t, err)
	}
	_, err = knowledge.GetKnowledgeByID(fixture.Key("A", 1, 3, false), "public-doc")
	require.Error(t, err, "old Key is not expanded")
	for _, ctx := range []context.Context{fixture.Web("B", 99), fixture.Key("B", 99, 6, false)} {
		_, err = knowledge.GetKnowledgeByID(ctx, "public-doc")
		require.NoError(t, err, "normal member/Web reads remain valid")
	}
	for _, ctx := range []context.Context{c, fixture.Key("D", 99, 7, false), fixture.Key("D", 99, 8, false), context.Background()} {
		_, err = knowledge.GetKnowledgeByID(ctx, "sensitive")
		require.Error(t, err)
		_, err = chunks.GetChunkByIDOnly(ctx, "sensitive-chunk")
		require.Error(t, err)
	}
	machine := types.WithPrincipal(types.WithTenantAPIKeyScope(fixture.Web("D", 99), types.TenantAPIKeyScope{KeyID: 4, ScopeType: types.APIKeyScopeTenant, FullAccess: true}), types.Principal{Type: types.PrincipalAPITenant, ID: "synthetic-machine"})
	_, err = knowledge.GetKnowledgeByID(machine, "public-doc")
	require.Error(t, err)
	_, err = knowledge.GetKnowledgeByID(d, "sensitive")
	require.NoError(t, err)
	body, err := chunks.GetChunkByIDOnly(d, "sensitive-chunk")
	require.NoError(t, err)
	require.Equal(t, fixture.Canary, body.Content)
	_, err = knowledge.GetKnowledgeByID(d, "blocked")
	require.Error(t, err)
	_, err = knowledge.GetKnowledgeByID(fixture.Web("D", 99), "blocked")
	require.NoError(t, err, "MCP restriction is distinct from authorized Web read")
	page, err := knowledge.ListPagedKnowledgeByKnowledgeBaseID(a, "public-kb", &types.Pagination{}, types.KnowledgeListFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), fixture.Canary)
	page, err = knowledge.ListPagedKnowledgeByKnowledgeBaseID(a, "public-kb", &types.Pagination{}, types.KnowledgeListFilter{Keyword: fixture.Canary})
	require.NoError(t, err)
	require.Zero(t, page.Total, "count is constrained before SQL pagination")
	page, err = knowledge.ListPagedKnowledgeByKnowledgeBaseID(c, "private-kb", &types.Pagination{}, types.KnowledgeListFilter{})
	require.NoError(t, err)
	require.Zero(t, page.Total, "empty allow-set cannot become ALL")
	page, err = knowledge.ListPagedKnowledgeByKnowledgeBaseID(d, "private-kb", &types.Pagination{}, types.KnowledgeListFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	batch, err := knowledge.GetKnowledgeBatch(a, 1, []string{"public-doc", "denied-doc", "sensitive"})
	require.NoError(t, err)
	require.Len(t, batch, 1)
	_, err = knowledge.GetKnowledgeBatch(a, 1, make([]string, access.MaxDocumentAllowSet+1))
	require.Error(t, err, "oversized input is rejected, never truncated or widened")
	_, err = chunks.GetRepository().ListChunksByIDOnly(a, make([]string, access.MaxDocumentAllowSet+1))
	require.Error(t, err)
	_, err = knowledge.GetRepository().GetKnowledgeByIDOnly(a, "sensitive")
	require.Error(t, err, "raw repository escape hatch is held")
	_, err = chunks.GetRepository().ListChunksByIDOnly(c, []string{"sensitive-chunk"})
	require.Error(t, err)
	_, err = chunks.ListChunkRevisions(d, "sensitive-chunk")
	require.Error(t, err, "historical content remains HOLD")
	_, err = knowledge.GetKnowledgeTags(d, []string{"sensitive"})
	require.Error(t, err, "unversioned tag dependencies remain HOLD")
	_, _, err = knowledge.GetKnowledgeFile(d, "sensitive")
	require.Error(t, err)
	f.Subject(t, "sensitive", "D", "revoked", 0)
	_, err = knowledge.GetKnowledgeByID(d, "sensitive")
	require.Error(t, err)
	_, err = chunks.ListChunksByKnowledgeID(d, "sensitive")
	require.Error(t, err)
}

func TestDocumentM5ContentIsFetchedAfterGrantVersion(t *testing.T) {
	for _, kind := range []string{"knowledge", "chunk"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture.New(t)
			policy := NewDocumentReadPolicy(true, f.Reader)
			knowledge := &knowledgeService{repo: repository.NewKnowledgeRepository(f.DB), documentACL: policy}
			chunks := &chunkService{chunkRepository: repository.NewChunkRepository(f.DB), knowledgeRepo: knowledge.repo, documentACL: policy}
			var changed atomic.Bool
			table := "knowledges"
			if kind == "chunk" {
				table = "chunks"
			}
			require.NoError(t, f.DB.Callback().Query().After("gorm:query").Register("m5-concurrent-content-update", func(query *gorm.DB) {
				if query.Statement.Table != table || !changed.CompareAndSwap(false, true) {
					return
				}
				if kind == "knowledge" {
					require.NoError(t, f.DB.Exec("UPDATE knowledges SET title=?,updated_at=updated_at WHERE id='public-doc'", "NEW_M5_APPROVED_VERSION").Error)
				} else {
					require.NoError(t, f.DB.Exec("UPDATE chunks SET content=?,updated_at=updated_at WHERE id='public-doc-chunk'", "NEW_M5_APPROVED_VERSION").Error)
				}
				f.Approve(t, "space", "99", "inherit", "ordinary")
				f.Approve(t, "kb", "public-kb", "inherit", "ordinary")
				f.Approve(t, "knowledge", "public-doc", "inherit", "ordinary")
			}))
			ctx := fixture.Key("A", 1, 1, true)
			if kind == "knowledge" {
				row, err := knowledge.GetKnowledgeByID(ctx, "public-doc")
				require.NoError(t, err)
				require.Equal(t, "NEW_M5_APPROVED_VERSION", row.Title)
			} else {
				row, err := chunks.GetChunkByIDOnly(ctx, "public-doc-chunk")
				require.NoError(t, err)
				require.Equal(t, "NEW_M5_APPROVED_VERSION", row.Content)
			}
			require.True(t, changed.Load())
		})
	}
}

func TestDocumentM5BatchNeighborAndParentOwnership(t *testing.T) {
	f := fixture.New(t)
	policy := NewDocumentReadPolicy(true, f.Reader)
	knowledge := &knowledgeService{repo: repository.NewKnowledgeRepository(f.DB), documentACL: policy}
	chunks := &chunkService{chunkRepository: repository.NewChunkRepository(f.DB), knowledgeRepo: knowledge.repo, documentACL: policy}
	require.NoError(t, f.DB.Exec("INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,content_revision,is_enabled,chunk_index,start_at,end_at,parent_chunk_id,status) VALUES('public-child',99,'public-kb','public-doc',?,'text',1,true,1,100,200,'public-doc-chunk',0)", fixture.PublicText).Error)
	f.Approve(t, "space", "99", "inherit", "ordinary")
	f.Approve(t, "kb", "public-kb", "inherit", "ordinary")
	f.Approve(t, "knowledge", "public-doc", "inherit", "ordinary")
	a := fixture.Key("A", 1, 1, true)
	d := fixture.Key("D", 99, 4, false)
	batch, err := chunks.GetRepository().ListChunksByIDOnly(a, []string{"public-doc-chunk", "public-child", "public-child"})
	require.NoError(t, err)
	require.Len(t, batch, 2)
	batch, err = chunks.GetRepository().ListChunksByIDOnly(d, []string{"sensitive-chunk"})
	require.NoError(t, err)
	require.Len(t, batch, 1)
	batch, err = chunks.GetRepository().ListChunksByIDOnly(d, []string{"sensitive-chunk", "other-sensitive-chunk"})
	require.Error(t, err)
	require.Nil(t, batch, "mixed unauthorized batch emits nothing")
	neighbors, err := chunks.GetRepository().ListChunkNeighbors(a, 99, "public-doc", 0, 0, 2, []types.ChunkType{types.ChunkTypeText})
	require.NoError(t, err)
	require.Len(t, neighbors, 1)
	require.Equal(t, "public-child", neighbors[0].ID)
	for _, tc := range []struct {
		ctx    context.Context
		tenant uint64
		doc    string
		after  int
	}{
		{a, 1, "public-doc", 2}, {fixture.Key("C", 99, 2, false), 99, "sensitive", 2}, {a, 99, "public-doc", 33},
	} {
		neighbors, err = chunks.GetRepository().ListChunkNeighbors(tc.ctx, tc.tenant, tc.doc, 0, 0, tc.after, []types.ChunkType{types.ChunkTypeText})
		require.Error(t, err)
		require.Nil(t, neighbors)
	}
	children, err := chunks.ListChunkByParentID(a, 99, "public-doc-chunk")
	require.NoError(t, err)
	require.Len(t, children, 1)
	_, err = chunks.ListChunkByParentID(a, 1, "public-doc-chunk")
	require.Error(t, err)
	require.NoError(t, f.DB.Exec("UPDATE chunks SET content=?,updated_at=updated_at WHERE id='public-child'", fixture.Canary).Error)
	neighbors, err = chunks.GetRepository().ListChunkNeighbors(a, 99, "public-doc", 0, 0, 2, []types.ChunkType{types.ChunkTypeText})
	require.Error(t, err)
	require.Nil(t, neighbors, "old worker writes invalidate existing public approval immediately")
}

func TestDocumentM5SQLScopeRevocationAndConcurrency(t *testing.T) {
	f := fixture.New(t)
	s := &knowledgeService{repo: repository.NewKnowledgeRepository(f.DB), documentACL: NewDocumentReadPolicy(true, f.Reader)}
	a := fixture.Key("A", 1, 1, true)
	d := fixture.Key("D", 99, 4, false)
	ctx, err := s.DocumentKBReadContext(d, "private-kb")
	require.NoError(t, err)
	_, _, err = access.GuardDocumentReadQuery(types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "A", Role: types.TenantRoleViewer}), 99, "private-kb")
	require.Error(t, err)
	_, _, err = access.GuardDocumentReadQuery(ctx, 1, "private-kb")
	require.Error(t, err)
	failures := make(chan error, 20)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			row, err := s.GetKnowledgeByID(a, "public-doc")
			if err == nil && row.TenantID != 99 {
				err = access.ErrForbidden
			}
			failures <- err
		}()
		go func() { defer wg.Done(); _, err := s.GetKnowledgeByID(d, "sensitive"); failures <- err }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	f.Subject(t, "sensitive", "D", "revoked", 0)
	_, _, err = s.repo.ListPagedKnowledgeByKnowledgeBaseID(ctx, 99, "private-kb", &types.Pagination{}, types.KnowledgeListFilter{})
	require.Error(t, err, "revoked opaque scope cannot be reused as a cached lease")
	require.NoError(t, f.DB.Exec("UPDATE chunks SET content=?,updated_at=updated_at WHERE id='public-doc-chunk'", "CHANGED_M5_BODY").Error)
	_, err = s.GetKnowledgeByID(a, "public-doc")
	require.Error(t, err, "raw worker write invalidates approval without a timestamp change")
}

func TestDocumentM5ClosedPolicyAndModelPathsHOLD(t *testing.T) {
	s := &knowledgeService{documentACL: NewDocumentReadPolicy(true, nil)}
	_, err := s.GetKnowledgeByID(fixture.Web("A", 1), "anything")
	require.Error(t, err)
	k := &knowledgeBaseService{documentACL: NewDocumentReadPolicy(true, nil)}
	_, err = k.GetQueryEmbedding(context.Background(), "private-kb", "query")
	require.Error(t, err, "no model service is consulted before a safe candidate path exists")
	_, err = s.SearchFAQEntries(context.Background(), "private-kb", nil)
	require.Error(t, err)
}
