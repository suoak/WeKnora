package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type mvpUnavailableIndexModel struct{ mvpChatAPI }

func (*mvpUnavailableIndexModel) GetModelID() string   { return "mvp-unavailable-index-model" }
func (*mvpUnavailableIndexModel) GetModelName() string { return "mvp-unavailable-index-model" }

func (*mvpUnavailableIndexModel) Chat(
	context.Context,
	[]chat.Message,
	*chat.ChatOptions,
) (*types.ChatResponse, error) {
	return nil, errors.New("isolated model unavailable")
}

func TestMVPWikiFinalizeMustNotSwallowGenerationFailurePostgres(t *testing.T) {
	for _, previous := range []string{"", "existing human introduction"} {
		t.Run(previous, func(t *testing.T) {
			db := syncdb.Open(t, "postgres")
			require.NoError(t, db.AutoMigrate(&types.WikiPage{}, &types.WikiPageRevision{}))
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
			ctx = context.WithValue(ctx, wikiFirstAddFinalizeKey{}, true)
			repo := repository.NewWikiPageRepository(db)
			pages := NewWikiPageService(repo, nil, nil, nil, nil, nil)
			page := &types.WikiPage{
				ID:              "index",
				TenantID:        1,
				KnowledgeBaseID: "kb",
				Slug:            "index",
				PageType:        types.WikiPageTypeIndex,
				Content:         previous,
			}
			_, err := pages.CreatePage(ctx, page)
			require.NoError(t, err)
			s := &wikiIngestService{wikiService: pages}
			err = s.rebuildIndexPage(
				ctx,
				&mvpUnavailableIndexModel{},
				WikiIngestPayload{KnowledgeBaseID: "kb"},
				"added first document",
				"English",
				"",
			)
			require.ErrorContains(
				t,
				err,
				"isolated model unavailable",
				"finalize must retain its durable lane instead of reporting a fallback "+
					"as success",
			)
			actual, err := repo.GetBySlug(ctx, "kb", "index")
			require.NoError(t, err)
			require.Equal(t, previous, actual.Content)
			require.Equal(t, page.Version, actual.Version)
		})
	}
}

// Reconstructing the service between failures must preserve the retry budget
// and the failure receipt, even if the ordinary finalizing counter is drained.
func TestMVPWikiRetryExhaustionRemainsVisibleAfterServiceRestartPostgres(t *testing.T) {
	f := newMVPFixture(t)
	require.NoError(t, f.db.AutoMigrate(&types.TaskDeadLetter{}))
	m := f.prepare("https://tenant.feishu.cn/file/real")
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	k, _, _ := mvpPublicTask(t, f, m)
	require.NoError(
		t,
		f.db.Model(k).
			Updates(map[string]any{"parse_status": types.ParseStatusFinalizing, "pending_subtasks_count": 1}).
			Error,
	)
	pending := repository.NewTaskPendingOpsRepository(f.db)
	op := &types.TaskPendingOp{
		TenantID: 1,
		TaskType: wikiTaskType,
		Scope:    wikiTaskScope,
		ScopeID:  "kb",
		DedupKey: k.ID,
		Op:       WikiOpIngest,
	}
	require.NoError(t, pending.Enqueue(f.ctx, op))
	for failure := 1; failure <= wikiMaxFailRetries+1; failure++ {
		s := &wikiIngestService{
			pendingRepo:    repository.NewTaskPendingOpsRepository(f.db),
			deadLetterRepo: repository.NewTaskDeadLetterRepository(f.db),
			knowledgeRepo:  f.ks.repo,
			knowledgeSvc:   mvpLiveKnowledge{repo: f.ks.repo},
		}
		require.NoError(
			t,
			s.requeueFailedOps(
				f.ctx,
				WikiIngestPayload{TenantID: 1, KnowledgeBaseID: "kb"},
				[]WikiPendingOp{{Op: WikiOpIngest, KnowledgeID: k.ID, dbID: op.ID}},
			),
		)
		var n int64
		require.NoError(t, f.db.Model(&types.TaskPendingOp{}).Where("id = ?", op.ID).Count(&n).Error)
		if failure <= wikiMaxFailRetries {
			require.EqualValues(t, 1, n)
			var saved types.TaskPendingOp
			require.NoError(t, f.db.Where("id = ?", op.ID).First(&saved).Error)
			require.Equal(t, failure, saved.FailCount)
		} else {
			require.Zero(t, n)
		}
	}
	var dead types.TaskDeadLetter
	require.NoError(
		t,
		f.db.Where("related_id = ? AND task_type = ?", k.ID, wikiTaskType).First(&dead).Error,
	)
	require.Equal(t, wikiMaxFailRetries+1, dead.FailCount)
	// Status projection must prefer this durable failure over parse completed.
	require.NoError(
		t,
		f.db.Create(
			&types.TaskPendingOp{
				TenantID: 1,
				TaskType: "knowledge:index_receipt",
				Scope:    personalIndexReceiptScope,
				ScopeID:  k.ID,
				DedupKey: "index-" + m.AttemptID,
				Op:       "verified",
			},
		).Error,
	)
	f.s.kbs = dispatchActualKB{
		kb: &types.KnowledgeBase{
			ID:               "kb",
			TenantID:         1,
			IndexingStrategy: types.IndexingStrategy{WikiEnabled: true},
		},
	}
	rows, err := f.s.ListImports(f.ctx, "kb")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "needs_attention", rows[0].Status)
	require.Equal(t, "attention", rows[0].WikiStatus)
	mvpAssertSingle(t, f, m)
}

func TestMVPWikiMetaLinkDependenciesPostgres(t *testing.T) {
	for _, writer := range []string{"meta_source_refs", "automatic_link_body", "direct_sql_body", "manual_body"} {
		t.Run(writer, func(t *testing.T) {
			db := syncdb.Open(t, "postgres")
			require.NoError(t, db.AutoMigrate(&types.WikiPage{}, &types.WikiPageRevision{}))
			ctx := context.Background()
			repo := repository.NewWikiPageRepository(db)
			svc := NewWikiPageService(repo, nil, nil, nil, nil, nil)
			page := &types.WikiPage{
				ID:              "P",
				TenantID:        1,
				KnowledgeBaseID: "kb",
				Slug:            "shared",
				Title:           "shared",
				PageType:        types.WikiPageTypeEntity,
				Status:          types.WikiPageStatusPublished,
				Content:         "original",
				SourceRefs:      types.StringArray{"Kold|Old"},
			}
			_, err := svc.CreatePage(ctx, page)
			require.NoError(t, err)
			stale, err := svc.GetPageBySlug(ctx, "kb", page.Slug)
			require.NoError(t, err)
			fresh, err := svc.GetPageBySlug(ctx, "kb", page.Slug)
			require.NoError(t, err)
			switch writer {
			case "meta_source_refs":
				fresh.SourceRefs = append(fresh.SourceRefs, "Kother|Other")
				require.NoError(t, repo.UpdateMeta(ctx, fresh))
			case "automatic_link_body":
				fresh.Content = "original [[another]]"
				fresh.OutLinks = types.StringArray{"another"}
				require.NoError(t, repo.UpdateAutoLinkedContent(ctx, fresh))
			case "direct_sql_body":
				require.NoError(
					t,
					db.Model(&types.WikiPage{}).
						Where("id = ?", page.ID).
						Update("content", "DIRECT original").
						Error,
				)
			case "manual_body":
				fresh.Content = "HUMAN original"
				_, err = svc.UpdatePage(ctx, fresh)
				require.NoError(t, err)
			}
			before, err := repo.GetBySlug(ctx, "kb", page.Slug)
			require.NoError(t, err)
			if writer != "manual_body" {
				require.Equal(
					t,
					stale.Version,
					before.Version,
					"this contract must detect changes that do not bump revision",
				)
			}
			stale.Content = "stale model result"
			_, err = svc.UpdatePage(ctx, stale)
			require.ErrorIs(t, err, repository.ErrWikiPageConflict)
			// Repository is an independent atomic boundary, not only a service check.
			require.ErrorIs(t, repo.UpdateWithRevision(ctx, stale, nil), repository.ErrWikiPageConflict)
			require.ErrorIs(t, repo.UpdateMeta(ctx, stale), repository.ErrWikiPageConflict)
			require.ErrorIs(t, repo.UpdateAutoLinkedContent(ctx, stale), repository.ErrWikiPageConflict)
			after, err := repo.GetBySlug(ctx, "kb", page.Slug)
			require.NoError(t, err)
			require.Equal(t, before.Content, after.Content)
			require.Equal(t, before.SourceRefs, after.SourceRefs)
			require.Equal(t, before.Version, after.Version)
		})
	}
}

func TestMVPWikiDependencyCheckAndCommitAreAtomicPostgres(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	require.NoError(t, db.AutoMigrate(&types.WikiPage{}))
	ctx := context.Background()
	repo := repository.NewWikiPageRepository(db)
	p := &types.WikiPage{ID: "P", KnowledgeBaseID: "kb", Slug: "shared", Content: "original", Version: 1}
	require.NoError(t, repo.Create(ctx, p))
	stale, err := repo.GetBySlug(ctx, "kb", p.Slug)
	require.NoError(t, err)
	stale.Content = "stale"
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	require.NoError(t, tx.Exec("UPDATE wiki_pages SET content = 'HUMAN' WHERE id = ?", p.ID).Error)
	pid := make(chan int, 1)
	done := make(chan error, 1)
	go func() {
		done <- db.Connection(func(conn *gorm.DB) error {
			var n int
			if err := conn.Raw("SELECT pg_backend_pid()").Scan(&n).Error; err != nil {
				return err
			}
			pid <- n
			return repository.NewWikiPageRepository(conn).Update(ctx, stale)
		})
	}()
	workerPID := <-pid
	require.Eventually(t, func() bool {
		var count int64
		return db.Raw("SELECT count(*) FROM pg_stat_activity WHERE pid = ? AND "+
			"wait_event_type = 'Lock'", workerPID).
			Scan(&count).
			Error == nil &&
			count == 1
	}, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, tx.Commit().Error)
	require.ErrorIs(t, <-done, repository.ErrWikiPageConflict)
	actual, err := repo.GetBySlug(ctx, "kb", p.Slug)
	require.NoError(t, err)
	require.Equal(t, "HUMAN", actual.Content)
}

func TestMVPWikiFirstAddFreshRegenerationAndIdempotentContributionPostgres(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	require.NoError(
		t,
		db.AutoMigrate(&types.WikiPage{}, &types.WikiPageRevision{}, &types.Knowledge{}, &types.Chunk{}),
	)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	kr := repository.NewKnowledgeRepository(db)
	cr := repository.NewChunkRepository(db)
	repo := repository.NewWikiPageRepository(db)
	pages := NewWikiPageService(repo, cr, nil, nil, nil, nil)
	require.NoError(
		t,
		kr.CreateKnowledge(
			ctx,
			&types.Knowledge{
				ID:              "Knew",
				TenantID:        1,
				KnowledgeBaseID: "kb",
				Channel:         "feishu_personal",
				Metadata:        types.JSON(`{"feishu_mvp_attempt":"first-attempt"}`),
				ParseStatus:     types.ParseStatusProcessing,
			},
		),
	)
	p := &types.WikiPage{
		ID:              "P",
		TenantID:        1,
		KnowledgeBaseID: "kb",
		Slug:            "entity/shared",
		Title:           "shared",
		Content:         "OLD SOURCE MUST SURVIVE",
		SourceRefs:      types.StringArray{"Kold|Old"},
		PageType:        types.WikiPageTypeEntity,
		Status:          types.WikiPageStatusPublished,
	}
	_, err := pages.CreatePage(ctx, p)
	require.NoError(t, err)
	s := &wikiIngestService{wikiService: pages, chunkRepo: cr, knowledgeSvc: mvpLiveKnowledge{repo: kr}}
	batch := &WikiBatchContext{
		SlugTitleMany:   func(context.Context, []string) map[string]string { return map[string]string{} },
		PlannedFolderID: map[string]string{},
	}
	updates := []SlugUpdate{
		{
			Slug:        p.Slug,
			Type:        "entity",
			KnowledgeID: "Knew",
			SourceRef:   "Knew|New",
			DocTitle:    "New",
			Item:        extractedItem{Name: "shared", Details: "new fact"},
		},
	}
	model := &mvpEditDuringModel{response: "SUMMARY: first addition\nNEW CONTRIBUTION"}
	model.edit = func() {
		human, e := pages.GetPageBySlug(ctx, "kb", p.Slug)
		require.NoError(t, e)
		human.Content += "\nHUMAN EDIT"
		human.SourceRefs = append(human.SourceRefs, "Kother|Other")
		_, e = pages.UpdatePage(ctx, human)
		require.NoError(t, e)
	}
	_, _, _, err = s.reduceSlugUpdates(ctx, model, "kb", p.Slug, updates, 1, batch, nil)
	require.ErrorIs(t, err, repository.ErrWikiPageConflict)
	// The same durable operation is retried, but reducer reads fresh dependencies.
	_, _, _, err = s.reduceSlugUpdates(ctx, model, "kb", p.Slug, updates, 1, batch, nil)
	require.NoError(t, err)
	actual, err := repo.GetBySlug(ctx, "kb", p.Slug)
	require.NoError(t, err)
	for _, text := range []string{"OLD SOURCE MUST SURVIVE", "HUMAN EDIT", "NEW CONTRIBUTION"} {
		require.Contains(t, actual.Content, text)
	}
	for _, ref := range []string{"Kold|Old", "Kother|Other", "Knew|New"} {
		require.Contains(t, actual.SourceRefs, ref)
	}
	before := actual.Content
	changed, _, _, err := s.reduceSlugUpdates(ctx, model, "kb", p.Slug, updates, 1, batch, nil)
	require.NoError(t, err)
	require.False(t, changed)
	actual, err = repo.GetBySlug(ctx, "kb", p.Slug)
	require.NoError(t, err)
	require.Equal(t, before, actual.Content)
	require.Equal(t, 1, strings.Count(actual.Content, "NEW CONTRIBUTION"))
}
