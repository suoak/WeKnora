package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/models/chat"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestLocalSummarySourceIsolation(t *testing.T) {
	testLocalSummarySourceIsolation(t, fixture.New(t))
}

func testLocalSummarySourceIsolation(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db := types.EnableLegacyReadGuards(f.DB)
	ctx := types.WithExecutionTenant(context.Background(), 1)
	require.NoError(t, db.Create(&types.DataSource{
		ID: "other-kb-held-source", TenantID: 1, KnowledgeBaseID: "private-home", Type: "feishu",
		Status: "active", LegacyAccessState: "hold",
	}).Error)
	gate := repository.NewModelRepository(db).(legacyModelInputGate)
	require.ErrorIs(t, gate.CheckLegacyModelInputs(ctx, 1), types.ErrLegacySourceRestricted)
	var knowledge types.Knowledge
	var chunks []*types.Chunk
	require.NoError(t, db.First(&knowledge, "id='public-two-doc'").Error)
	require.NoError(t, db.Where("knowledge_id='public-two-doc'").Find(&chunks).Error)
	messages := []chat.Message{{Role: "system", Content: "summarize"}, {Role: "user", Content: chunks[0].Content}}
	options := &chat.ChatOptions{MaxTokens: 100}
	svc := &knowledgeService{repo: repository.NewKnowledgeRepository(db), chunkRepo: repository.NewChunkRepository(db)}
	proof, err := svc.localSummaryMessages(ctx, &knowledge, chunks, messages, options)
	require.NoError(t, err)
	spy := &legacySpyModel{}
	model := &legacyGuardedChat{inner: spy, gate: gate, tenant: 1}
	_, err = model.Chat(proof, messages, options)
	require.NoError(t, err, "an unrelated Hold must not stop a verified local summary")
	require.EqualValues(t, 1, spy.calls.Load())
	_, err = model.Chat(ctx, messages, options)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted, "tenant-wide guard is still enforced")
	changed := append([]chat.Message(nil), messages...)
	changed[1].Content = "unverified extra input"
	_, err = model.Chat(proof, changed, options)
	require.Error(t, err)
	_, err = model.Chat(proof, messages, &chat.ChatOptions{MaxTokens: 200})
	require.Error(t, err)
	otherTenant := &legacyGuardedChat{inner: spy, gate: gate, tenant: 99}
	_, err = otherTenant.Chat(proof, messages, options)
	require.Error(t, err)
	require.EqualValues(t, 1, spy.calls.Load())
	stale := knowledge
	stale.CustomMetadata = types.JSON(`{"stale":"unverified metadata"}`)
	_, err = svc.localSummaryMessages(ctx, &stale, chunks, messages, options)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)

	for _, mutation := range []string{
		`UPDATE chunks SET content='changed' WHERE id='public-two-chunk'`,
		`UPDATE chunks SET is_enabled=false WHERE id='public-two-chunk'`,
		`DELETE FROM chunks WHERE id='public-two-chunk'`,
		`UPDATE knowledges SET file_hash='changed' WHERE id='public-two-doc'`,
		`UPDATE knowledges SET custom_metadata='{"changed":"yes"}' WHERE id='public-two-doc'`,
		`UPDATE knowledges SET channel='feishu_personal' WHERE id='public-two-doc'`,
		`UPDATE knowledges SET enable_status='disabled' WHERE id='public-two-doc'`,
		`UPDATE knowledge_bases SET deleted_at=CURRENT_TIMESTAMP WHERE id='public-two'`,
		`UPDATE tenants SET status='disabled' WHERE id=1`,
		`UPDATE data_sources SET knowledge_base_id='public-two' WHERE id='other-kb-held-source'`,
	} {
		t.Run(mutation, func(t *testing.T) {
			tx := db.Begin()
			require.NoError(t, tx.Error)
			defer tx.Rollback()
			scoped := &knowledgeService{repo: repository.NewKnowledgeRepository(tx), chunkRepo: repository.NewChunkRepository(tx)}
			p, e := scoped.localSummaryMessages(ctx, &knowledge, chunks, messages, options)
			require.NoError(t, e)
			require.NoError(t, tx.Exec(mutation).Error)
			_, e = model.Chat(p, messages, options)
			require.Error(t, e, "stale, revoked, or replaced input must never reach the provider")
			require.EqualValues(t, 1, spy.calls.Load())
		})
	}
	t.Run("revoked_during_provider_call", func(t *testing.T) {
		tx := db.Begin()
		require.NoError(t, tx.Error)
		defer tx.Rollback()
		scoped := &knowledgeService{repo: repository.NewKnowledgeRepository(tx), chunkRepo: repository.NewChunkRepository(tx)}
		p, e := scoped.localSummaryMessages(ctx, &knowledge, chunks, messages, options)
		require.NoError(t, e)
		provider := &localSummaryMutatingModel{legacySpyModel: &legacySpyModel{}, mutate: func() {
			require.NoError(t, tx.Exec(`UPDATE chunks SET is_enabled=false WHERE id='public-two-chunk'`).Error)
		}}
		guard := &legacyGuardedChat{inner: provider, gate: gate, tenant: 1}
		response, e := guard.Chat(p, messages, options)
		require.Error(t, e)
		require.Nil(t, response, "withdrawn output must be discarded after the provider returns")
		require.EqualValues(t, 1, provider.calls.Load())
	})

	// A connector must never acquire local-document authority by changing its channel.
	require.NoError(t, db.Exec(`INSERT INTO task_pending_ops(tenant_id,scope,scope_id,task_type,op,dedup_key,payload)
 VALUES(1,?,'public-two-doc','receipt','published','synthetic-receipt','{}')`, types.PersonalExactReceiptScope).Error)
	_, err = svc.localSummaryMessages(ctx, &knowledge, chunks, messages, options)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
}

type localSummaryMutatingModel struct {
	*legacySpyModel
	mutate func()
}

func (m *localSummaryMutatingModel) Chat(
	ctx context.Context, messages []chat.Message, options *chat.ChatOptions,
) (*types.ChatResponse, error) {
	response, err := m.legacySpyModel.Chat(ctx, messages, options)
	m.mutate()
	return response, err
}

func TestWikiInputChangesRetryWithoutPermanentBlock(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	repo := repository.NewWikiPageRepository(f.DB)
	proof, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	require.NoError(t, f.DB.Exec(`UPDATE chunks SET content='current verified revision'
 WHERE id='public-two-chunk'`).Error)
	page := &types.WikiPage{
		ID: "input-race-page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/input-race",
		PageType: "concept", Status: "published", Content: "stale model output", Version: 1,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	err = repo.Create(proof, page)
	require.ErrorIs(t, err, repository.ErrWikiMCPInputsChanged)
	require.NotErrorIs(t, err, repository.ErrWikiMCPUnverified)
	var count int64
	require.NoError(t, f.DB.Model(&types.WikiPage{}).Where("id=?", page.ID).Count(&count).Error)
	require.Zero(t, count, "a stale generation must not publish")
	service := &wikiIngestService{wikiService: &wikiPageService{repo: repo}}
	service.recordWikiBlock(context.Background(), WikiIngestPayload{TenantID: 1, KnowledgeBaseID: "public-two"}, &err)
	block, readErr := repository.ReadWikiProcessingBlock(context.Background(), repo, "public-two")
	require.NoError(t, readErr)
	require.True(t, block == nil || block.Reason == "", "input races must remain normal retries")
	fresh, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	page.Content = "current revision output"
	require.NoError(t, repo.Create(fresh, page))
}
