package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestWikiProductionGitLabUsesCurrentBindingAndRetainsVersionFence(t *testing.T) {
	testWikiProductionGitLab(t, fixture.New(t))
}

func testWikiProductionGitLab(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db := f.DB
	require.NoError(t, db.Exec(`INSERT INTO data_sources(
 id,tenant_id,knowledge_base_id,name,type,status,legacy_access_state,config)
 VALUES('gitlab',1,'public-two','synthetic','gitlab','active','unmanaged','{}')`).Error)
	require.NoError(t, db.Exec(`UPDATE knowledges SET channel='gitlab',
 metadata='{"datasource_id":"gitlab","external_id":"gitlab:synthetic:main:doc.md"}' WHERE id='public-two-doc'`).Error)
	repo := repository.NewWikiPageRepository(db)
	proof, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	page := &types.WikiPage{
		ID: "gitlab-page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "summary/gitlab",
		PageType: "summary", Status: "published", Content: "synthetic generated content", Version: 1,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.NoError(t, repo.Create(proof, page))
	verified, _, err := (&repository.PersonalWikiStore{DB: db}).VerifiedPages(
		context.Background(), 1, "public-two", []string{"public-two-doc"},
	)
	require.NoError(t, err)
	require.Len(t, verified, 1, "connector proof must not masquerade as a document ID")
	require.NoError(t, db.Exec(`UPDATE data_sources SET config='{"branch":"changed"}' WHERE id='gitlab'`).Error)
	page.Content = "must reject stale input"
	require.Error(t, repo.Update(proof, page))
	for _, mutation := range []string{
		`UPDATE data_sources SET status='paused' WHERE id='gitlab'`,
		`UPDATE data_sources SET status='active',legacy_access_state='hold' WHERE id='gitlab'`,
		`UPDATE data_sources SET legacy_access_state='revoked' WHERE id='gitlab'`,
		`UPDATE data_sources SET legacy_access_state='unmanaged',tenant_id=99 WHERE id='gitlab'`,
	} {
		require.NoError(t, db.Exec(mutation).Error)
		_, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
		require.Error(t, err)
	}
}

func TestWikiProductionMetadataScopeDoesNotGrantSourceBodies(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	db := types.EnableLegacyReadGuards(f.DB)
	require.NoError(t, db.Exec(`INSERT INTO data_sources(
 id,tenant_id,knowledge_base_id,name,type,status,legacy_access_state,config)
 VALUES('held',1,'public-two','synthetic','feishu','active','hold','{}')`).Error)
	var kb types.KnowledgeBase
	require.ErrorIs(t, db.First(&kb, "id='public-two'").Error, types.ErrLegacySourceRestricted)
	ctx := types.WithLegacyKBAdminMetadata(context.Background(), "public-two")
	require.NoError(t, db.WithContext(ctx).First(&kb, "id='public-two'").Error)
	var doc types.Knowledge
	require.ErrorIs(t, db.WithContext(ctx).First(&doc, "id='public-two-doc'").Error, types.ErrLegacySourceRestricted)
	var chunk types.Chunk
	require.ErrorIs(t, db.WithContext(ctx).First(&chunk, "knowledge_id='public-two-doc'").Error,
		types.ErrLegacySourceRestricted)
	var other types.KnowledgeBase
	wrong := types.WithLegacyKBAdminMetadata(context.Background(), "wrong-kb")
	require.ErrorIs(t, db.WithContext(wrong).First(&other, "id='public-two'").Error, types.ErrLegacySourceRestricted)
}

type wikiExistenceKnowledge struct {
	interfaces.KnowledgeService
	row *types.Knowledge
	err error
}

func (s wikiExistenceKnowledge) GetKnowledgeByIDOnly(context.Context, string) (*types.Knowledge, error) {
	return s.row, s.err
}

func TestWikiProductionUnknownReadNeverDeletesOrSettlesUpdates(t *testing.T) {
	for _, denied := range []error{
		types.ErrLegacySourceRestricted, types.ErrExactDocumentScope, errors.New("database unavailable"),
	} {
		s := &wikiIngestService{knowledgeSvc: wikiExistenceKnowledge{err: denied}}
		gone, err := s.isKnowledgeGone(context.Background(), "kb", "doc")
		require.False(t, gone)
		require.ErrorIs(t, err, denied)
		_, err = s.filterLiveUpdates(context.Background(), "kb", []SlugUpdate{{KnowledgeID: "doc", Type: "addition"}})
		require.ErrorIs(t, err, denied)
	}
	s := &wikiIngestService{knowledgeSvc: wikiExistenceKnowledge{err: repository.ErrKnowledgeNotFound}}
	gone, err := s.isKnowledgeGone(context.Background(), "kb", "doc")
	require.NoError(t, err)
	require.True(t, gone)
}

func TestWikiProductionBlockRetainsOriginalOperationAndStopsRetry(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	require.NoError(t, f.DB.AutoMigrate(&types.TaskPendingOp{}))
	op := types.TaskPendingOp{
		TenantID: 1, TaskType: "wiki:ingest", Scope: "knowledge_base", ScopeID: "public-two",
		Op: "ingest", DedupKey: "public-two-doc", Payload: []byte(`{"knowledge_id":"public-two-doc"}`),
	}
	require.NoError(t, f.DB.Create(&op).Error)
	repo := repository.NewWikiPageRepository(f.DB)
	s := &wikiIngestService{wikiService: &wikiPageService{repo: repo}}
	err := error(types.ErrLegacySourceRestricted)
	s.recordWikiBlock(context.Background(), WikiIngestPayload{TenantID: 1, KnowledgeBaseID: "public-two"}, &err)
	require.ErrorIs(t, err, asynq.SkipRetry)
	require.ErrorIs(t, s.wikiBlocked(context.Background(), "public-two"), asynq.SkipRetry)
	var retained types.TaskPendingOp
	require.NoError(t, f.DB.First(&retained, op.ID).Error)
	require.Equal(t, op.Payload, retained.Payload)
	require.Nil(t, retained.ClaimedAt)
	require.Zero(t, retained.FailCount)
	require.NoError(t, s.clearWikiBlock(context.Background(),
		WikiIngestPayload{TenantID: 1, KnowledgeBaseID: "public-two"}))
	require.NoError(t, s.wikiBlocked(context.Background(), "public-two"))
}
