package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type sharedWikiReadStub struct {
	agentSharedWikiStore
	pages, linked []*types.WikiPage
	partial       bool
	read          func()
	proofErr      error
	calls         int
	validated     int
}

func (s *sharedWikiReadStub) VerifiedPatternSearchPages(
	_ context.Context, tenant uint64, kb string, _ []string, _ string,
) ([]*types.WikiPage, bool, error) {
	s.calls++
	if tenant != 1 || kb != "public-two" {
		return nil, false, errors.New("wrong owner scope")
	}
	if s.read != nil {
		s.read()
	}
	return s.pages, s.partial, nil
}

func (s *sharedWikiReadStub) SharedWikiDocumentIDs(context.Context, uint64, string) ([]string, error) {
	return []string{"public-two-doc"}, nil
}

func (s *sharedWikiReadStub) VerifiedLinkedPages(
	context.Context, uint64, string, []string, []string,
) ([]*types.WikiPage, bool, error) {
	return s.linked, false, nil
}

func (s *sharedWikiReadStub) RevalidatePages(
	_ context.Context, _ uint64, _ string, _ []string, pages []*types.WikiPage,
) error {
	s.validated += len(pages)
	return s.proofErr
}

func sharedWikiReaderFixture(store agentSharedWikiStore) (*agentSharedWikiReader, *fakeKBShareService) {
	kb := &types.KnowledgeBase{
		ID: "public-two", TenantID: 1, IndexingStrategy: types.IndexingStrategy{WikiEnabled: true},
	}
	shares := &fakeKBShareService{allowedKBs: map[string]bool{kb.ID: true}}
	base := &wikiPageService{kbService: &wikiGuardKBService{kb: kb}, kbShareService: shares}
	return &agentSharedWikiReader{WikiPageService: base, service: base, store: store}, shares
}

func TestAgentSharedWikiRequiresGrantAndFreshPageProof(t *testing.T) {
	for _, scenario := range []string{"allowed", "denied", "revoked-during-read", "changed-source", "foreign-page"} {
		t.Run(scenario, func(t *testing.T) {
			page := &types.WikiPage{
				ID: "page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/one",
				Title: "one", Content: "verified", Status: types.WikiPageStatusPublished,
			}
			store := &sharedWikiReadStub{pages: []*types.WikiPage{page}}
			reader, shares := sharedWikiReaderFixture(store)
			switch scenario {
			case "denied":
				shares.allowedKBs["public-two"] = false
			case "revoked-during-read":
				store.read = func() { shares.allowedKBs["public-two"] = false }
			case "changed-source":
				store.proofErr = repository.ErrWikiMCPInputsChanged
			case "foreign-page":
				page.TenantID = 3
			}
			pages, _, err := reader.SearchPagesWithStatus(fixture.Web("A", 2), "public-two", "one", 10)
			if scenario == "allowed" {
				require.NoError(t, err)
				require.Len(t, pages, 1)
				require.Equal(t, 1, store.validated)
			} else {
				require.Error(t, err)
				require.Empty(t, pages)
			}
			if scenario == "denied" {
				require.Zero(t, store.calls, "denial must precede page reads")
			}
		})
	}
}

func TestAgentSharedWikiPreservesOnlyVerifiedLinkLabelsAndPartialStatus(t *testing.T) {
	page := &types.WikiPage{
		ID: "page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/one", Title: "one",
		Content: "[[concept/visible|DPD]] [[concept/hidden|SECRET_NAME]]", Status: types.WikiPageStatusPublished,
		OutLinks: types.StringArray{"concept/hidden"},
	}
	linked := &types.WikiPage{
		ID: "link", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/visible",
		Title: "visible", Status: types.WikiPageStatusPublished,
	}
	store := &sharedWikiReadStub{pages: []*types.WikiPage{page}, linked: []*types.WikiPage{linked}, partial: true}
	reader, _ := sharedWikiReaderFixture(store)
	pages, partial, err := reader.SearchPagesWithStatus(fixture.Web("A", 2), "public-two", "one", 10)
	require.NoError(t, err)
	require.True(t, partial)
	require.Len(t, pages, 1)
	require.Contains(t, pages[0].Content, "[[concept/visible|DPD]]")
	require.NotContains(t, pages[0].Content, "SECRET_NAME")
	require.Empty(t, pages[0].OutLinks)
	require.Equal(t, 2, store.validated, "linked label dependencies need the final proof too")
	require.Contains(t, page.Content, "SECRET_NAME", "proof objects must remain immutable")
	_, err = reader.SearchPages(fixture.Web("A", 2), "public-two", "one", 10)
	require.ErrorIs(t, err, repository.ErrWikiMCPUnverified, "legacy callers cannot hide partial status")
}

func TestAgentSharedWikiPostgres(t *testing.T) {
	if os.Getenv("WIKI_HISTORY_POSTGRES_DSN") == "" {
		t.Skip("requires isolated migrated PostgreSQL")
	}
	f, store, _, sealed, _ := wikiReadSessionWithFixture(t, wikiPendingRegenerationFixture(t))
	for i := 0; i < 205; i++ {
		require.NoError(t, f.DB.Create(&types.WikiPage{
			ID: fmt.Sprintf("native-old-%03d", i), TenantID: 1, KnowledgeBaseID: "public-two",
			Slug: fmt.Sprintf("aaa/%03d", i), Title: "irrelevant", Content: "unsealed historical text",
			Status: types.WikiPageStatusPublished, PageType: types.WikiPageTypeConcept, Version: 1,
		}).Error)
	}
	ctx := fixture.Web("A", 2)
	base := &wikiPageService{repo: repository.NewWikiPageRepository(f.DB)}
	hidden, err := base.SearchPages(ctx, "public-two", "session", 10)
	require.NoError(t, err)
	require.Empty(t, hidden, "reproduce the tenant-local browser filter on a shared KB")
	reader, _ := sharedWikiReaderFixture(store)
	pages, partial, err := reader.SearchPagesWithStatus(ctx, "public-two", "sess.*", 10)
	require.NoError(t, err)
	require.False(t, partial)
	require.Len(t, pages, 1)
	require.Equal(t, sealed[0].ID, pages[0].ID)
	read, err := reader.GetPageBySlug(ctx, "public-two", sealed[0].Slug)
	require.NoError(t, err)
	require.Equal(t, sealed[0].Content, read.Content)
	require.NoError(t, f.DB.Exec("UPDATE wiki_pages SET content='RAW_UNSEALED' WHERE id=?", sealed[0].ID).Error)
	pages, partial, err = reader.SearchPagesWithStatus(ctx, "public-two", "sess.*", 10)
	require.NoError(t, err)
	require.True(t, partial)
	require.Empty(t, pages)
	_, err = reader.GetPageBySlug(ctx, "public-two", sealed[0].Slug)
	require.Error(t, err, "a raw page edit must never gain serving permission")
}
