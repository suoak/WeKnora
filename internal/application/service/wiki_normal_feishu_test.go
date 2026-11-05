package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWikiNormalFeishuRequiresPublishedResourceAndCurrentSource(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	db := f.DB
	require.NoError(t, db.Exec(`INSERT INTO data_sources
 (id,tenant_id,knowledge_base_id,name,type,status,legacy_access_state,config)
 VALUES('normal-feishu',1,'public-two','synthetic','feishu','active','unmanaged','{}')`).Error)
	require.NoError(t, db.Exec(`UPDATE knowledges SET channel='feishu' WHERE id='public-two-doc'`).Error)
	repo := repository.NewWikiPageRepository(db)
	_, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted, "normal admission alone never proves document content")
	require.NoError(t, db.Exec(`INSERT INTO feishu_legacy_resources
 (id,tenant_id,knowledge_base_id,data_source_id,external_id,knowledge_id,state,active_attempt)
 VALUES('normal-resource',1,'public-two','normal-feishu','synthetic',
 'public-two-doc','active','published-attempt')`).Error)
	proof, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err, "verified normal publications do not require a historical source recovery")
	page := &types.WikiPage{
		ID: "normal-page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/normal",
		PageType: "concept", Status: "published", Content: "synthetic generated content", Version: 1,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.NoError(t, repo.Create(proof, page))
	for _, state := range []string{"hold", "revoked"} {
		require.NoError(t, db.Exec(`UPDATE data_sources SET legacy_access_state=?
 WHERE id='normal-feishu'`, state).Error)
		_, err = repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
		require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
		require.Error(t, repo.Update(proof, page), "a previous generation cannot publish after source withdrawal")
	}
	require.NoError(t, db.Exec(`UPDATE data_sources SET legacy_access_state='unmanaged'
 WHERE id='normal-feishu'`).Error)
	require.NoError(t, db.Exec(`UPDATE feishu_legacy_resources SET active_attempt='' WHERE id='normal-resource'`).Error)
	_, err = repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
}
