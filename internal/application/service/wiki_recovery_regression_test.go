package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type recoveryIndexRepository struct {
	interfaces.WikiPageRepository
	page   *types.WikiPage
	err    error
	writes int
}

func (r *recoveryIndexRepository) GetBySlug(context.Context, string, string) (*types.WikiPage, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.page == nil {
		return nil, repository.ErrWikiPageNotFound
	}
	return r.page, nil
}

func (r *recoveryIndexRepository) Create(context.Context, *types.WikiPage) error {
	r.writes++
	return errors.New("directory read attempted a write")
}

func (*recoveryIndexRepository) ListByTypeLight(
	context.Context, string, string, int, int,
) ([]types.WikiIndexEntry, int64, error) {
	return []types.WikiIndexEntry{{Slug: "entity/source", Title: "Source"}}, 1, nil
}

func TestWikiRecoveryIndexViewDoesNotCreateUnverifiedIndex(t *testing.T) {
	repo := &recoveryIndexRepository{}
	svc := &wikiPageService{repo: repo}
	// This also works while a worker is between capturing a proof and writing
	// its first page: no KB service or write dependencies are needed by GET.
	for range 2 {
		view, err := svc.GetIndexView(context.Background(), "kb", []string{"entity"}, 50, "")
		require.NoError(t, err)
		require.Empty(t, view.Intro)
		require.Zero(t, view.Version)
		require.Len(t, view.Groups, 1)
		require.Equal(t, "entity/source", view.Groups[0].Items[0].Slug)
	}
	require.Zero(t, repo.writes)
}

func TestWikiRecoveryIndexViewPreservesExistingIntroAndErrors(t *testing.T) {
	repo := &recoveryIndexRepository{page: &types.WikiPage{Content: "Verified introduction", Version: 7}}
	svc := &wikiPageService{repo: repo}
	view, err := svc.GetIndexView(context.Background(), "kb", []string{"entity"}, 50, "")
	require.NoError(t, err)
	require.Equal(t, "Verified introduction", view.Intro)
	require.Equal(t, 7, view.Version)
	repo.err = errors.New("database unavailable")
	_, err = svc.GetIndexView(context.Background(), "kb", []string{"entity"}, 50, "")
	require.ErrorContains(t, err, "database unavailable")
	require.Zero(t, repo.writes)
}

func TestWikiRecoveryPublicationBudgetSurvivesCancelledModelContext(t *testing.T) {
	type key string
	original, cancel := context.WithCancel(context.WithValue(context.Background(), key("proof"), "retained"))
	cancel()
	ctx, done := wikiIngestPostProcessContext(original, 200)
	defer done()
	require.NoError(t, ctx.Err())
	require.Equal(t, "retained", ctx.Value(key("proof")))
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.Greater(t, time.Until(deadline), 6*time.Minute)
	require.LessOrEqual(t, time.Until(deadline), 10*time.Minute)
	cleanup, closeCleanup := wikiIngestCleanupContext(original)
	defer closeCleanup()
	cleanupDeadline, ok := cleanup.Deadline()
	require.True(t, ok)
	require.LessOrEqual(t, time.Until(cleanupDeadline), 10*time.Second)
	done()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestWikiRecoveryPublicationBudgetIsBoundedForOversizedBatch(t *testing.T) {
	for _, n := range []int{-1, 0, 1, 200, int(^uint(0) >> 1)} {
		ctx, cancel := wikiIngestPostProcessContext(context.Background(), n)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.Greater(t, time.Until(deadline), 20*time.Second)
		require.LessOrEqual(t, time.Until(deadline), 10*time.Minute)
		cancel()
	}
}

type recoverySlowPublisher struct {
	interfaces.WikiPageService
	published int
}

func (*recoverySlowPublisher) GetPageBySlug(context.Context, string, string) (*types.WikiPage, error) {
	return &types.WikiPage{Status: types.WikiPageStatusDraft}, nil
}

func (p *recoverySlowPublisher) UpdatePageMeta(ctx context.Context, page *types.WikiPage) error {
	if p.published == 0 {
		// Real proved-page publication can exceed the old ten-second budget.
		select {
		case <-time.After(11 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if page.Status != types.WikiPageStatusPublished {
		return errors.New("page was not published")
	}
	p.published++
	return nil
}

func TestWikiRecoveryPublicationCompletesAfterOldCleanupDeadline(t *testing.T) {
	publisher := &recoverySlowPublisher{}
	worker := &wikiIngestService{wikiService: publisher}
	ctx, cancel := wikiIngestPostProcessContext(context.Background(), 2)
	defer cancel()
	worker.publishDraftPages(ctx, "kb", []string{"entity/a", "entity/b"})
	require.Equal(t, 2, publisher.published)
}
