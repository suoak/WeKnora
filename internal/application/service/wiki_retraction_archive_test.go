package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWikiOrphanRetractionPreservesProofAndHistory(t *testing.T) {
	testWikiOrphanRetraction(t, fixture.New(t))
}

func testWikiOrphanRetraction(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db := f.DB
	require.NoError(t, db.AutoMigrate(&types.WikiPageRevision{}))
	repo := repository.NewWikiPageRepository(db)
	ctx, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	pages := NewWikiPageService(repo, nil, nil, nil, nil, nil)
	page := &types.WikiPage{
		ID: "orphan-canary", TenantID: 1, KnowledgeBaseID: "public-two",
		Slug: "concept/orphan", Title: "canary", Content: "ORIGINAL VERIFIED BODY",
		PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusDraft,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	_, err = pages.CreatePage(ctx, page)
	require.NoError(t, err)
	svc := &wikiIngestService{
		wikiService:  pages,
		knowledgeSvc: mvpLiveKnowledge{repo: repository.NewKnowledgeRepository(db)},
	}
	batch := &WikiBatchContext{
		SlugTitleMany: func(context.Context, []string) map[string]string { return map[string]string{} },
	}
	changed, _, _, err := svc.reduceSlugUpdates(ctx, nil, "public-two", page.Slug, []SlugUpdate{
		{Slug: page.Slug, Type: "retractStale", KnowledgeID: "public-two-doc"},
	}, 1, batch, nil)
	require.NoError(t, err, "last-source archival must not call a model")
	require.True(t, changed)
	archived, err := repo.GetBySlug(ctx, "public-two", page.Slug)
	require.NoError(t, err)
	require.Equal(t, types.WikiPageStatusArchived, archived.Status)
	require.Equal(t, page.Content, archived.Content)
	require.Equal(t, page.SourceRefs, archived.SourceRefs)
	revisions, total, err := repo.ListRevisions(ctx, "public-two", page.ID, 10, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, revisions, 1)
	ctx, err = repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err, "a retained archive must not poison the next generation")
	invalid := &types.WikiPage{
		ID: "uncited-canary", TenantID: 1, KnowledgeBaseID: "public-two",
		Slug: "concept/uncited", PageType: types.WikiPageTypeConcept, Content: "NO SOURCE",
		Status: types.WikiPageStatusDraft, SourceRefs: types.StringArray{},
	}
	require.ErrorIs(t, repo.Create(ctx, invalid), repository.ErrWikiMCPUnverified)
	var count int64
	require.NoError(t, db.Model(&types.WikiPage{}).Where("id=?", invalid.ID).Count(&count).Error)
	require.Zero(t, count, "an uncited write must roll back atomically")
	human := *archived
	human.LastEditSource = types.WikiEditSourceUser
	require.NoError(t, restoreWikiRetractionArchive(&human))
	require.Equal(t, types.WikiPageStatusArchived, human.Status, "respect a subsequent human archive")
	model := &mvpEditDuringModel{response: "SUMMARY: restored\nNEW VERIFIED CONTRIBUTION"}
	changed, _, _, err = svc.reduceSlugUpdates(ctx, model, "public-two", page.Slug, []SlugUpdate{
		{
			Slug: page.Slug, Type: types.WikiPageTypeConcept, KnowledgeID: "public-two-doc",
			SourceRef: "public-two-doc", DocTitle: "canary", Item: extractedItem{Name: "canary", Details: "new fact"},
		},
	}, 1, batch, nil)
	require.NoError(t, err)
	require.True(t, changed)
	restored, err := repo.GetBySlug(ctx, "public-two", page.Slug)
	require.NoError(t, err)
	require.Equal(t, types.WikiPageStatusDraft, restored.Status)
	require.Contains(t, restored.Content, "NEW VERIFIED CONTRIBUTION")
	_, err = repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
}
