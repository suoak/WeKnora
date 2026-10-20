package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type approvedIndexModel struct {
	mvpChatAPI
	calls  int
	during func()
}

func (*approvedIndexModel) GetModelID() string   { return "isolated-index-authoring" }
func (*approvedIndexModel) GetModelName() string { return "isolated-index-authoring" }
func (m *approvedIndexModel) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	m.calls++
	if m.during != nil {
		m.during()
	}
	return &types.ChatResponse{Content: "A new automatically generated introduction."}, nil
}

func TestMVPWikiIndexAuthoringProtectionPostgres(t *testing.T) {
	for _, source := range []string{types.WikiEditSourcePipeline, types.WikiEditSourceUser, types.WikiEditSourceAgent, types.WikiEditSourceRevert, ""} {
		name := source
		if name == "" {
			name = "unknown_legacy"
		}
		t.Run(name, func(t *testing.T) {
			db := syncdb.Open(t, "postgres")
			require.NoError(t, db.AutoMigrate(&types.WikiPage{}, &types.WikiPageRevision{}))
			ctx := context.WithValue(context.Background(), wikiFirstAddFinalizeKey{}, true)
			repo := repository.NewWikiPageRepository(db)
			pages := NewWikiPageService(repo, nil, nil, nil, nil, nil)
			original := "Keep this existing introduction intact."
			page := &types.WikiPage{ID: "index", KnowledgeBaseID: "kb", Slug: "index", PageType: types.WikiPageTypeIndex, Content: original, Summary: original, Version: 8, LastEditSource: source}
			// Direct fixture insertion preserves the unknown legacy source exactly.
			require.NoError(t, db.Create(page).Error)
			model := &approvedIndexModel{}
			svc := &wikiIngestService{wikiService: pages}
			require.NoError(t, svc.rebuildIndexPage(ctx, model, WikiIngestPayload{KnowledgeBaseID: "kb"}, "added source", "English", ""))
			actual, err := repo.GetBySlug(ctx, "kb", "index")
			require.NoError(t, err)
			var history []types.WikiPageRevision
			require.NoError(t, db.Where("page_id = ?", page.ID).Find(&history).Error)
			if source == types.WikiEditSourcePipeline {
				require.Equal(t, 1, model.calls)
				require.NotEqual(t, original, actual.Content)
				require.Equal(t, 9, actual.Version)
				require.Len(t, history, 1)
				require.Equal(t, original, history[0].Content)
			} else {
				require.Equal(t, original, actual.Content, "human, reverted, agent or unknown introduction must survive first-add finalize")
				require.Equal(t, original, actual.Summary)
				require.Equal(t, 8, actual.Version)
				require.Equal(t, source, actual.LastEditSource)
				require.Zero(t, model.calls, "protected introduction must not be sent for rewriting")
				require.Empty(t, history)
			}
		})
	}
}

func TestMVPWikiIndexHumanEditDuringGenerationPostgres(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	require.NoError(t, db.AutoMigrate(&types.WikiPage{}, &types.WikiPageRevision{}))
	ctx := context.WithValue(context.Background(), wikiFirstAddFinalizeKey{}, true)
	repo := repository.NewWikiPageRepository(db)
	pages := NewWikiPageService(repo, nil, nil, nil, nil, nil)
	page := &types.WikiPage{ID: "index", KnowledgeBaseID: "kb", Slug: "index", PageType: types.WikiPageTypeIndex, Content: "Original pipeline introduction", Version: 1, LastEditSource: types.WikiEditSourcePipeline}
	require.NoError(t, db.Create(page).Error)
	model := &approvedIndexModel{during: func() {
		fresh, err := pages.GetPageBySlug(ctx, "kb", "index")
		require.NoError(t, err)
		fresh.Content = "HUMAN introduction committed during generation"
		_, err = pages.UpdatePage(types.WithWikiEditSource(ctx, types.WikiEditSourceUser), fresh)
		require.NoError(t, err)
	}}
	svc := &wikiIngestService{wikiService: pages}
	require.ErrorIs(t, svc.rebuildIndexPage(ctx, model, WikiIngestPayload{KnowledgeBaseID: "kb"}, "added source", "English", ""), repository.ErrWikiPageConflict)
	actual, err := repo.GetBySlug(ctx, "kb", "index")
	require.NoError(t, err)
	require.Equal(t, "HUMAN introduction committed during generation", actual.Content)
	require.Equal(t, types.WikiEditSourceUser, actual.LastEditSource)
}
