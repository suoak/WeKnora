package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type retiredFinalizePages struct {
	durableFinalizePages
	failure error
	marker  bool
}

func (s *retiredFinalizePages) GetPageBySlug(_ context.Context, _ string, slug string) (*types.WikiPage, error) {
	if s.failure != nil {
		return nil, s.failure
	}
	if slug == "retired" {
		return nil, fmt.Errorf("retired target: %w", repository.ErrWikiPageNotFound)
	}
	page := &types.WikiPage{Slug: slug, Status: types.WikiPageStatusPublished}
	if s.marker {
		page.PageMetadata = types.JSON(`{"feishu_first_add":{"current":"receipt"}}`)
	}
	if slug == "archived" {
		page.Status = types.WikiPageStatusArchived
	}
	return page, nil
}

func TestWikiFinalizeMarkerDiscoverySkipsOnlyRetiredTargets(t *testing.T) {
	ctx := context.Background()
	pages := &retiredFinalizePages{marker: true}
	svc := &wikiIngestService{wikiService: pages}
	found, err := svc.hasFirstAddPages(ctx, "kb", []string{"retired", "archived", "current"})
	require.NoError(t, err)
	require.True(t, found, "a missing historical target must not hide a current first addition")
	found, err = svc.hasFirstAddPages(ctx, "kb", []string{"retired", "archived"})
	require.NoError(t, err)
	require.False(t, found, "retired bodies cannot supply current first-add evidence")
	for _, failure := range []error{
		errors.New("database unavailable"), types.ErrLegacySourceRestricted,
		repository.ErrWikiMCPUnverified,
	} {
		pages.failure = fmt.Errorf("lookup refused: %w", failure)
		found, err = svc.hasFirstAddPages(ctx, "kb", []string{"current"})
		require.False(t, found)
		require.ErrorIs(t, err, failure, "authority and database failures must still stop convergence")
	}
}

type retiredFinalizeModel struct {
	mvpUnavailableIndexModel
	calls int
}

func (m *retiredFinalizeModel) Chat(
	context.Context, []chat.Message, *chat.ChatOptions,
) (*types.ChatResponse, error) {
	m.calls++
	return &types.ChatResponse{Content: "Current verified introduction."}, nil
}

func TestWikiFinalizeRetiredSlugConvergesWithoutRestoringHistoricalBody(t *testing.T) {
	data, err := json.Marshal(wikiFinalizeRow{Slug: "retired"})
	require.NoError(t, err)
	change, err := json.Marshal(wikiFinalizeRow{Change: &wikiFinalizeChange{
		Action: wikiFinalizeAdded, DocTitle: "current document", DocSummary: "current verified content",
	}})
	require.NoError(t, err)
	pending := &folderPrunePendingRepoStub{rows: []*types.TaskPendingOp{
		{ID: 7, Op: wikiFinalizeOpSlug, Payload: data},
		{ID: 8, Op: wikiFinalizeOpChange, Payload: change},
	}}
	pages := &retiredFinalizePages{}
	model := &retiredFinalizeModel{}
	svc := &wikiIngestService{
		wikiService: pages, pendingRepo: pending, kbService: &durableFinalizeKB{model: "model"},
		modelService: &durableFinalizeModels{model: model}, task: &folderPruneTaskStub{},
	}
	err = svc.ProcessWikiFinalize(context.Background(), asynq.NewTask(types.TypeWikiFinalize,
		[]byte(`{"tenant_id":1,"knowledge_base_id":"kb"}`)))
	require.NoError(t, err)
	require.Equal(t, []int64{7, 8}, pending.deletedRowIDs, "retire rows only after actual current index generation")
	require.Equal(t, 1, model.calls)
	require.Equal(t, 1, pages.updates)
	require.Equal(t, data, []byte(pending.rows[0].Payload))
}
