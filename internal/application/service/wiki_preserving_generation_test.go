package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWikiPreservingGenerationKeepsOldPageUntilVerifiedReplacement(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	db := types.EnableLegacyReadGuards(f.DB)
	require.NoError(t, db.AutoMigrate(&types.WikiPageRevision{}))
	repo := repository.NewWikiPageRepository(db)
	old := &types.WikiPage{
		ID: "historical", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "summary/current",
		Title: "Historical", Content: "HISTORICAL_BODY_NEVER_MODEL_INPUT", PageType: "summary",
		Status: "published", Version: 3, SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.NoError(t, db.Create(old).Error)
	_, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.Error(t, err)
	ctx, err := repository.BeginWikiPreservingGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	_, err = repo.GetBySlug(ctx, "public-two", old.Slug)
	require.ErrorIs(t, err, repository.ErrWikiPageNotFound, "old text cannot enter model reads")
	var visible int64
	require.NoError(t, db.WithContext(ctx).Model(&types.WikiPage{}).Count(&visible).Error)
	require.Zero(t, visible, "lightweight model queries must also exclude historical rows")
	var retained types.WikiPage
	require.NoError(t, db.First(&retained, "id=?", old.ID).Error)
	require.Equal(t, old.Content, retained.Content)
	require.False(t, retained.DeletedAt.Valid, "capture must not hide browser pages")
	replacement := &types.WikiPage{
		ID: "new-candidate", TenantID: 1, KnowledgeBaseID: "public-two", Slug: old.Slug,
		Title: "Verified replacement", Content: "freshly generated only from current verified inputs",
		PageType: "summary", Status: "published", Version: 1,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	pages := &wikiPageService{repo: repo}
	_, err = pages.CreatePage(ctx, replacement)
	require.NoError(t, err, "the production page service must support atomic replacement")
	require.Equal(t, old.ID, replacement.ID)
	require.Equal(t, old.Version+1, replacement.Version)
	current, err := repo.GetBySlug(ctx, "public-two", old.Slug)
	require.NoError(t, err, "committed output joins the live generation read set")
	require.Equal(t, replacement.Content, current.Content)
	require.NoError(t, db.WithContext(ctx).Model(&types.WikiPage{}).Count(&visible).Error)
	require.EqualValues(t, 1, visible)
	var revision types.WikiPageRevision
	require.NoError(t, db.First(&revision, "page_id=? AND version=?", old.ID, old.Version).Error)
	require.Equal(t, old.Content, revision.Content)
	_, err = repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err, "replacement has real current provenance")
}

func TestWikiPreservingGenerationRejectsChangedInputsWithoutReplacingPage(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	db := types.EnableLegacyReadGuards(f.DB)
	require.NoError(t, db.AutoMigrate(&types.WikiPageRevision{}))
	repo := repository.NewWikiPageRepository(db)
	old := &types.WikiPage{
		ID: "historical", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "summary/current",
		Content: "original historical text", PageType: "summary", Status: "published", Version: 2,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.NoError(t, db.Create(old).Error)
	ctx, err := repository.BeginWikiPreservingGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id='public-two-doc'").Update("title", "changed").Error)
	candidate := *old
	candidate.ID, candidate.Content = "new", "must not commit"
	require.Error(t, repo.Create(ctx, &candidate))
	var saved types.WikiPage
	require.NoError(t, db.First(&saved, "id=?", old.ID).Error)
	require.Equal(t, old.Content, saved.Content)
	var revisions int64
	require.NoError(t, db.Model(&types.WikiPageRevision{}).Count(&revisions).Error)
	require.Zero(t, revisions, "failed replacement must not add false history")
}

func TestWikiPreservingGenerationCannotOverwriteConcurrentHumanEdit(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	db := types.EnableLegacyReadGuards(f.DB)
	require.NoError(t, db.AutoMigrate(&types.WikiPageRevision{}))
	repo := repository.NewWikiPageRepository(db)
	old := &types.WikiPage{
		ID: "historical", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "summary/current",
		Content: "old page", PageType: "summary", Status: "published", Version: 2,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.NoError(t, db.Create(old).Error)
	ctx, err := repository.BeginWikiPreservingGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	require.NoError(t, db.Model(old).Update("content", "NEW_HUMAN_EDIT").Error)
	candidate := *old
	candidate.ID, candidate.Content = "candidate", "fresh candidate must not replace the human edit"
	require.Error(t, repo.Create(ctx, &candidate))
	var saved types.WikiPage
	require.NoError(t, db.First(&saved, "id=?", old.ID).Error)
	require.Equal(t, "NEW_HUMAN_EDIT", saved.Content)
	require.False(t, saved.DeletedAt.Valid)
}

func TestWikiPreservingGenerationRejectsNewSourceOutsideCapturedInventory(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	db := types.EnableLegacyReadGuards(f.DB)
	repo := repository.NewWikiPageRepository(db)
	ctx, err := repository.BeginWikiPreservingGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	newSource := &types.Knowledge{
		ID: "new-pending-source", TenantID: 1, KnowledgeBaseID: "public-two",
		Type: "file", ParseStatus: "pending", EnableStatus: "enabled",
	}
	require.NoError(t, db.Create(newSource).Error)
	gate := repository.NewModelRepository(db).(legacyModelInputGate)
	require.Error(t, gate.CheckLegacyModelInputs(ctx, 1), "never reuse the old selection after inventory growth")
	page := &types.WikiPage{
		ID: "must-not-publish", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "summary/stale",
		Title: "Stale", Content: "must not publish", PageType: "summary", Status: "published",
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.Error(t, repo.Create(ctx, page))
	var count int64
	require.NoError(t, db.Model(&types.WikiPage{}).Where("id=?", page.ID).Count(&count).Error)
	require.Zero(t, count)
}
