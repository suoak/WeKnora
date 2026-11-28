package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPortalWikiLineageUsesRecordedSourcesAndCanonicalChunkParents(t *testing.T) {
	for _, tc := range []struct {
		name, status, level string
		chunks              types.StringArray
	}{
		{"document_only", "complete", "document", nil},
		{"actual_chunk", "complete", "chunk", types.StringArray{"public-two-chunk"}},
		{"unrelated_and_missing", "partial", "chunk", types.StringArray{
			"public-two-chunk", "unreferenced-chunk", "private-home-chunk", "missing-chunk",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture.New(t)
			f.PreparePublished(t)
			require.NoError(t, f.DB.Exec(`INSERT INTO knowledges
(id,tenant_id,knowledge_base_id,type,title,source,file_name,parse_status,enable_status,channel)
VALUES('unreferenced-doc',1,'public-two','file','not a page citation',
'synthetic-local','unreferenced.txt','completed','enabled','web')`).Error)
			require.NoError(t, f.DB.Exec(`INSERT INTO chunks
(id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,content_revision,is_enabled,chunk_index,start_at,end_at)
VALUES('unreferenced-chunk',1,'public-two','unreferenced-doc','other source body','text',1,true,0,0,17)`).Error)
			repo := repository.NewWikiPageRepository(f.DB)
			gen, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
			require.NoError(t, err)
			page := &types.WikiPage{
				ID: "lineage-page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/lineage",
				Title: "lineage", Content: "technical fact", Status: types.WikiPageStatusPublished,
				PageType: types.WikiPageTypeConcept, Version: 7,
				SourceRefs: types.StringArray{"public-two-doc|forged display title"}, ChunkRefs: tc.chunks,
			}
			require.NoError(t, repo.Create(gen, page))
			service := publishedServiceM7(t, f)
			result, err := service.PersonalWiki(fixture.Key("A", 1, 1, true), "wiki_read_page", "",
				"public-two", page.Slug, 1, 0)
			require.NoError(t, err)
			require.Len(t, result.Pages, 1)
			lineage := result.Pages[0].Provenance
			require.NotNil(t, lineage)
			require.Equal(t, tc.status, lineage.Status)
			require.Equal(t, page.ID, lineage.PageID)
			require.Len(t, lineage.RevisionSHA256, 64)
			require.Equal(t, 1, lineage.SourceCount)
			require.Len(t, lineage.Sources, 1, "generation dependency closure is not a citation list")
			source := lineage.Sources[0]
			require.Equal(t, "public-two-doc", source.KnowledgeID)
			require.Equal(t, tc.level, source.Level)
			require.Len(t, source.VersionSHA256, 64)
			require.NotContains(t, source.Title, "forged")
			require.Equal(t, "knowhub:tenant:1:kb:public-two:document:public-two-doc", source.Reference)
			if tc.level == "chunk" {
				require.Len(t, source.Chunks, 1)
				require.Equal(t, "public-two-chunk", source.Chunks[0].ChunkID)
				require.Equal(t, source.Reference, source.Chunks[0].Reference)
			} else {
				require.Empty(t, source.Chunks, "summary attribution must not invent chunk evidence")
			}
			raw, err := json.Marshal(lineage)
			require.NoError(t, err)
			for _, denied := range []string{"unreferenced-doc", "unreferenced-chunk", "private-home", "missing-chunk"} {
				require.NotContains(t, string(raw), denied)
			}
		})
	}
}

type wikiLineageOutputObserver struct {
	*repository.PersonalWikiStore
	afterRead func()
}

func (w *wikiLineageOutputObserver) VerifiedPageLineage(
	ctx context.Context, tenant uint64, kb string, authorized []string, page *types.WikiPage,
) (*types.WikiPageLineage, error) {
	lineage, err := w.PersonalWikiStore.VerifiedPageLineage(ctx, tenant, kb, authorized, page)
	if err == nil {
		w.afterRead()
	}
	return lineage, err
}

func TestPortalWikiLineageRetainsFreshOutputFences(t *testing.T) {
	for name, mutation := range map[string]string{
		"page_changed":    "UPDATE wiki_pages SET content='changed' WHERE id='session-page'",
		"source_changed":  "UPDATE chunks SET content='changed' WHERE id='public-two-chunk'",
		"source_title":    "UPDATE knowledges SET title='changed' WHERE id='public-two-doc'",
		"source_disabled": "UPDATE knowledges SET enable_status='disabled' WHERE id='public-two-doc'",
		"key_revoked":     "UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1",
	} {
		t.Run(name, func(t *testing.T) {
			f, store, _, _, _ := wikiReadSessionFixture(t)
			service := publishedServiceM7(t, f)
			service.Wiki = &wikiLineageOutputObserver{
				PersonalWikiStore: store,
				afterRead:         func() { require.NoError(t, f.DB.Exec(mutation).Error) },
			}
			result, err := service.PersonalWiki(fixture.Key("A", 1, 1, true), "wiki_read_page", "",
				"public-two", "concept/session", 1, 0)
			require.Error(t, err)
			require.Nil(t, result, "source metadata must not survive a failed final page/source/key fence")
		})
	}
}
