package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fileService "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// This contract starts with the real P1-2 PG/manifest fixture. A downloaded
// manifest alone must not be reported as public, indexed Knowledge.
func TestMVPFirstCreateManifestMustCreateStablePublicKnowledge(t *testing.T) {
	t.Setenv("FEISHU_PERSONAL_MVP_FIRST_CREATE", "1")
	f := newPersonalRecoveryFixture(t)
	f.ks.fileSvc = fileService.NewLocalFileService(t.TempDir(), "")
	m := f.prepare("https://tenant.feishu.cn/file/real")
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	var k types.Knowledge
	require.NoError(t, f.db.Where("id = ?", m.LogicalKnowledgeID).First(&k).Error,
		"durable first-create must retain the accepted logical Knowledge ID")
	require.Equal(t, m.LogicalKnowledgeID, k.ID)
	var count int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NotEqual(t, types.ParseStatusCompleted, k.ParseStatus,
		"manifest download/dispatch is not proof of parse/index completion")
	var op types.TaskPendingOp
	require.NoError(
		t,
		f.db.Where("scope = ? AND scope_id = ?", types.KnowledgeDispatchScope, k.ID).First(&op).Error,
	)
	require.NotEqual(t, "completed", op.Op)
}

type mvpLiveKnowledge struct {
	interfaces.KnowledgeService
	repo interfaces.KnowledgeRepository
}

func (s mvpLiveKnowledge) GetKnowledgeByIDOnly(ctx context.Context, id string) (*types.Knowledge, error) {
	return s.repo.GetKnowledgeByIDOnly(ctx, id)
}

type (
	mvpChatAPI         = chat.Chat
	mvpEditDuringModel struct {
		mvpChatAPI
		edit     func()
		response string
	}
)

func (m *mvpEditDuringModel) GetModelID() string   { return "mvp-first-add-contract" }
func (m *mvpEditDuringModel) GetModelName() string { return "mvp-first-add-contract" }

func (m *mvpEditDuringModel) Chat(
	_ context.Context,
	_ []chat.Message,
	_ *chat.ChatOptions,
) (*types.ChatResponse, error) {
	if m.edit != nil {
		edit := m.edit
		m.edit = nil
		edit()
	}
	return &types.ChatResponse{Content: m.response}, nil
}

// Real PostgreSQL + real reducer/page service/repository. Only the remote LLM
// is controlled so a human commit lands exactly inside its request window.
func TestMVPFirstAddMustPreserveConcurrentManualEdit(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	require.NoError(
		t,
		db.AutoMigrate(
			&types.WikiPage{},
			&types.WikiPageRevision{},
			&types.WikiFolder{},
			&types.Knowledge{},
			&types.Chunk{},
		),
	)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	kr := repository.NewKnowledgeRepository(db)
	require.NoError(
		t,
		kr.CreateKnowledge(
			ctx,
			&types.Knowledge{
				ID:              "Knew",
				TenantID:        1,
				KnowledgeBaseID: "kb",
				Channel:         "feishu_personal",
				ParseStatus:     types.ParseStatusProcessing,
			},
		),
	)
	pr := repository.NewWikiPageRepository(db)
	cr := repository.NewChunkRepository(db)
	pages := NewWikiPageService(pr, cr, nil, nil, nil, nil)
	p := &types.WikiPage{
		ID:              "P",
		TenantID:        1,
		KnowledgeBaseID: "kb",
		Slug:            "entity/shared",
		Title:           "shared",
		Content:         "old source contribution",
		Summary:         "old",
		PageType:        types.WikiPageTypeEntity,
		Status:          types.WikiPageStatusPublished,
		SourceRefs:      types.StringArray{"Kold|Old"},
	}
	_, err := pages.CreatePage(ctx, p)
	require.NoError(t, err)
	model := &mvpEditDuringModel{
		response: "SUMMARY: combined\nold source contribution plus first Knew addition",
	}
	model.edit = func() {
		human, err := pages.GetPageBySlug(ctx, "kb", p.Slug)
		require.NoError(t, err)
		human.Content = "HUMAN: preserve this concurrent edit and old source contribution"
		human.SourceRefs = append(human.SourceRefs, "Kother|Other")
		_, err = pages.UpdatePage(types.WithWikiEditSource(ctx, types.WikiEditSourceUser), human)
		require.NoError(t, err)
	}
	ingest := &wikiIngestService{
		wikiService:  pages,
		chunkRepo:    cr,
		knowledgeSvc: mvpLiveKnowledge{repo: kr},
	}
	batch := &WikiBatchContext{
		SlugTitleMany:   func(context.Context, []string) map[string]string { return map[string]string{} },
		PlannedFolderID: map[string]string{},
	}
	_, _, _, reduceErr := ingest.reduceSlugUpdates(
		ctx,
		model,
		"kb",
		p.Slug,
		[]SlugUpdate{
			{
				Slug:        p.Slug,
				Type:        "entity",
				KnowledgeID: "Knew",
				SourceRef:   "Knew|New",
				DocTitle:    "New",
				Item:        extractedItem{Name: "shared", Details: "first addition"},
			},
		},
		1,
		batch,
		nil,
	)
	actual, err := pr.GetBySlug(ctx, "kb", p.Slug)
	require.NoError(t, err)
	require.Contains(
		t,
		actual.Content,
		"HUMAN:",
		"stale first-add LLM output must not replace a concurrent manual edit",
	)
	require.Contains(t, actual.SourceRefs, "Kother|Other")
	require.ErrorIs(
		t,
		reduceErr,
		repository.ErrWikiPageConflict,
		"a conflict must remain retryable, never count as success",
	)
	var n int64
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", "Knew").Count(&n).Error)
	require.EqualValues(t, 1, n)
}
