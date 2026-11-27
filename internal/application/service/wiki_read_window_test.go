package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWikiReadWindowExactSlugBeyondCandidateLimit(t *testing.T) {
	f, store, _, _, _ := wikiReadSessionFixture(t)
	r := repository.NewWikiPageRepository(f.DB)
	gen, err := repository.BeginWikiMCPGeneration(context.Background(), r, "public-two")
	require.NoError(t, err)
	target := &types.WikiPage{
		ID: "window-target", TenantID: 1, KnowledgeBaseID: "public-two",
		Slug: "zzz/target", Title: "target", Content: "verified target", Status: types.WikiPageStatusPublished,
		PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{"public-two-doc|source"}, Version: 1,
	}
	require.NoError(t, r.Create(gen, target))
	// Historical pages ahead of the requested slug cannot hide an authorized exact read.
	for i := 0; i < 205; i++ {
		page := &types.WikiPage{
			ID: fmt.Sprintf("window-old-%03d", i), TenantID: 1,
			KnowledgeBaseID: "public-two", Slug: fmt.Sprintf("aaa/%03d", i), Title: "old",
			Content: "unsealed", Status: types.WikiPageStatusPublished, PageType: types.WikiPageTypeConcept, Version: 1,
		}
		require.NoError(t, f.DB.Create(page).Error)
	}
	ctx := repository.WithWikiReadProofSession(fixture.Key("A", 1, 1, true))
	pages, partial, err := store.VerifiedPagesWindow(ctx, 1, "public-two", []string{"public-two-doc"}, target.Slug, 1)
	require.NoError(t, err)
	require.False(t, partial)
	require.Len(t, pages, 1)
	require.Equal(t, target.ID, pages[0].ID)
	require.NoError(t, store.RevalidatePages(ctx, 1, "public-two", []string{"public-two-doc"}, pages))
	require.NoError(t, f.DB.Exec("UPDATE chunks SET content='changed' WHERE knowledge_id='public-two-doc'").Error)
	ctx = repository.WithWikiReadProofSession(fixture.Key("A", 1, 1, true))
	pages, partial, err = store.VerifiedPagesWindow(ctx, 1, "public-two", []string{"public-two-doc"}, target.Slug, 1)
	require.NoError(t, err)
	require.True(t, partial)
	require.Empty(t, pages, "exact slug selection must retain live source proof")
}

func TestWikiReadWindowBoundsOutputAndRetainsAuthority(t *testing.T) {
	f, store, _, _, _ := wikiReadSessionFixture(t)
	r := repository.NewWikiPageRepository(f.DB)
	gen, err := repository.BeginWikiMCPGeneration(context.Background(), r, "public-two")
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		page := &types.WikiPage{
			ID: fmt.Sprintf("window-%d", i), TenantID: 1, KnowledgeBaseID: "public-two",
			Slug: fmt.Sprintf("concept/window-%d", i), Title: "window", Content: "verified",
			Status: types.WikiPageStatusPublished, PageType: types.WikiPageTypeConcept,
			SourceRefs: types.StringArray{"public-two-doc|source"}, Version: 1,
		}
		require.NoError(t, r.Create(gen, page))
	}
	ctx := repository.WithWikiReadProofSession(fixture.Key("A", 1, 1, true))
	pages, partial, err := store.VerifiedPagesWindow(ctx, 1, "public-two", []string{"public-two-doc"}, "", 2)
	require.NoError(t, err)
	require.False(t, partial, "ordinary output pagination is not a source verification failure")
	require.Len(t, pages, 2)
	require.NoError(t, store.RevalidatePages(ctx, 1, "public-two", []string{"public-two-doc"}, pages))
	require.Error(t, store.RevalidatePages(ctx, 1, "public-two", nil, pages))
	for _, limit := range []int{0, 201} {
		_, _, err = store.VerifiedPagesWindow(ctx, 1, "public-two", []string{"public-two-doc"}, "", limit)
		require.Error(t, err)
	}
}

type wikiWindowObserver struct {
	*repository.PersonalWikiStore
	slug        string
	limit       int
	bounded     int
	legacy      int
	revalidated int
	searched    int
}

func (w *wikiWindowObserver) VerifiedPagesWindow(
	ctx context.Context, tenant uint64, kb string, authorized []string, slug string, limit int,
) ([]*types.WikiPage, bool, error) {
	w.slug, w.limit = slug, limit
	w.bounded++
	return w.PersonalWikiStore.VerifiedPagesWindow(ctx, tenant, kb, authorized, slug, limit)
}

func (w *wikiWindowObserver) VerifiedPages(
	ctx context.Context, tenant uint64, kb string, authorized []string,
) ([]*types.WikiPage, bool, error) {
	w.legacy++
	return w.PersonalWikiStore.VerifiedPages(ctx, tenant, kb, authorized)
}

func (w *wikiWindowObserver) VerifiedSearchPages(
	ctx context.Context, tenant uint64, kb string, authorized []string, query string,
) ([]*types.WikiPage, bool, error) {
	w.searched++
	return w.PersonalWikiStore.VerifiedSearchPages(ctx, tenant, kb, authorized, query)
}

func (w *wikiWindowObserver) RevalidatePages(
	ctx context.Context, tenant uint64, kb string, authorized []string, pages []*types.WikiPage,
) error {
	w.revalidated += len(pages)
	return w.PersonalWikiStore.RevalidatePages(ctx, tenant, kb, authorized, pages)
}

func TestWikiReadWindowServiceUsesWindowAndKeepsFinalProof(t *testing.T) {
	f, store, _, _, _ := wikiReadSessionFixture(t)
	s := publishedServiceM7(t, f)
	observer := &wikiWindowObserver{PersonalWikiStore: store}
	s.Wiki = observer
	ctx := fixture.Key("A", 1, 1, true)
	result, err := s.PersonalWiki(ctx, "wiki_index", "", "public-two", "", 1, 0)
	require.NoError(t, err)
	require.Len(t, result.Pages, 1)
	require.Equal(t, 2, observer.limit, "index reads include one pagination lookahead")
	require.Equal(t, 1, observer.bounded)
	require.Equal(t, 1, observer.revalidated)
	result, err = s.PersonalWiki(ctx, "wiki_read_page", "", "public-two", "concept/session", 50, 0)
	require.NoError(t, err)
	require.Len(t, result.Pages, 1)
	require.Equal(t, "concept/session", observer.slug)
	require.Equal(t, 1, observer.limit)
	require.Equal(t, 2, observer.revalidated)
	result, err = s.PersonalWiki(ctx, "wiki_search", "verified", "public-two", "", 50, 0)
	require.NoError(t, err)
	require.Len(t, result.Pages, 1)
	require.Equal(t, 0, observer.legacy, "search selects matching candidates before its proof window")
	require.Equal(t, 1, observer.searched)
	require.Equal(t, 2, observer.bounded)
	require.Equal(t, 3, observer.revalidated)
}

func TestWikiSearchFindsLiteralMatchBeyondCandidateWindow(t *testing.T) {
	f, store, _, _, _ := wikiReadSessionFixture(t)
	r := repository.NewWikiPageRepository(f.DB)
	gen, err := repository.BeginWikiMCPGeneration(context.Background(), r, "public-two")
	require.NoError(t, err)
	target := &types.WikiPage{
		ID: "search-window-target", TenantID: 1, KnowledgeBaseID: "public-two",
		Slug: "zzz/search-target", Title: "接口 Case_%", Summary: "literal\\path",
		Content: "verified searchable target", Status: types.WikiPageStatusPublished,
		PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{"public-two-doc|source"}, Version: 1,
	}
	require.NoError(t, r.Create(gen, target))
	for i := 0; i < 205; i++ {
		page := &types.WikiPage{
			ID: fmt.Sprintf("search-window-old-%03d", i), TenantID: 1,
			KnowledgeBaseID: "public-two", Slug: fmt.Sprintf("aaa/%03d", i), Title: "irrelevant",
			Content: "unsealed history", Status: types.WikiPageStatusPublished,
			PageType: types.WikiPageTypeConcept, Version: 1,
		}
		require.NoError(t, f.DB.Create(page).Error)
	}
	for _, query := range []string{"接口 case_%", "literal\\path", "verified searchable", "Case_%\nliteral"} {
		ctx := repository.WithWikiReadProofSession(fixture.Key("A", 1, 1, true))
		pages, partial, err := store.VerifiedSearchPages(ctx, 1, "public-two", []string{"public-two-doc"}, query)
		require.NoError(t, err)
		require.False(t, partial)
		require.Len(t, pages, 1)
		require.Equal(t, target.ID, pages[0].ID)
		require.NoError(t, store.RevalidatePages(ctx, 1, "public-two", []string{"public-two-doc"}, pages))
	}
	ctx := repository.WithWikiReadProofSession(fixture.Key("A", 1, 1, true))
	pages, _, err := store.VerifiedSearchPages(ctx, 1, "public-two", nil, "verified searchable")
	require.NoError(t, err)
	require.Empty(t, pages, "a matching page never authorizes an out-of-scope source")
	pages, _, err = store.VerifiedSearchPages(ctx, 1, "public-two", []string{"public-two-doc"}, "%' OR 1=1 --")
	require.NoError(t, err)
	require.Empty(t, pages, "query syntax stays literal data")
	require.NoError(t, f.DB.Exec("UPDATE chunks SET content='changed' WHERE knowledge_id='public-two-doc'").Error)
	ctx = repository.WithWikiReadProofSession(fixture.Key("A", 1, 1, true))
	pages, partial, err := store.VerifiedSearchPages(ctx, 1, "public-two", []string{"public-two-doc"}, "verified searchable")
	require.NoError(t, err)
	require.True(t, partial)
	require.Empty(t, pages, "matching text cannot bypass the current source proof")
}
