package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type publishedMembersM7 struct {
	interfaces.TenantMemberService
	repo interfaces.TenantMemberRepository
}

func (m publishedMembersM7) GetMembership(ctx context.Context, user string, tenant uint64) (*types.TenantMember, error) {
	return m.repo.Get(ctx, user, tenant)
}

func publishedServiceM7(t *testing.T, f *fixture.Fixture) *PortalMCPKnowledge {
	t.Helper()
	return NewPortalPersonalKnowledge(&config.Config{PersonalMCP: &config.PersonalMCPConfig{Enabled: true, PolicyReference: "ISOLATED_PUBLISHED_BUSINESS_RULE"}}, f.DB, publishedMembersM7{repo: repository.NewTenantMemberRepository(f.DB)}, repository.NewTenantAPIKeyRepository(f.DB), repository.NewKnowledgeBaseRepository(f.DB), repository.NewTenantRepository(f.DB), repository.NewKnowledgeRepository(f.DB), repository.NewChunkRepository(f.DB), NewModelService(repository.NewModelRepository(f.DB), repository.NewKnowledgeBaseRepository(f.DB), nil, nil, nil, nil), retriever.NewRetrieveEngineRegistry(nil, nil), nil, nil, nil)
}

func TestPortalM7PublishedDefaultDynamicNoApprovals(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	s := publishedServiceM7(t, f)
	require.NotNil(t, s)
	a := fixture.Key("A", 1, 1, true)
	ids := func() map[string]bool {
		ts, e := s.Resolve(a, nil)
		require.NoError(t, e)
		out := map[string]bool{}
		for _, x := range ts {
			out[x.Descriptor().ID] = x.Public()
		}
		return out
	}
	initial := ids()
	require.True(t, initial["public-kb"])
	require.True(t, initial["public-two"])
	private, ok := initial["private-home"]
	require.True(t, ok)
	require.False(t, private)
	require.NotContains(t, initial, "private-kb")
	var approved int64
	require.NoError(t, f.DB.Table("kb_publication_policies").Where("state='approved'").Count(&approved).Error)
	require.Zero(t, approved)
	require.NoError(t, f.DB.Exec("INSERT INTO tenants(id,name,business) VALUES(3,'future','synthetic')").Error)
	require.NoError(t, f.DB.Exec("INSERT INTO tenant_portal_configs(tenant_id,status,display_name,created_by,updated_by) VALUES(3,'draft','future','owner','owner')").Error)
	require.NoError(t, f.DB.Exec("INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,type) VALUES('future-kb','future',3,'future-model','synthetic','document')").Error)
	require.NotContains(t, ids(), "future-kb")
	require.NoError(t, f.DB.Exec("UPDATE tenant_portal_configs SET status='published' WHERE tenant_id=3").Error)
	require.Contains(t, ids(), "future-kb", "empty valid published KB is discovered without Key rebuild")
	require.NoError(t, f.DB.Exec("INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,type) VALUES('later-kb','later',3,'future-model','synthetic','document')").Error)
	require.Contains(t, ids(), "later-kb")
	require.NoError(t, f.DB.Exec("INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,file_name,parse_status,enable_status,channel) VALUES('sync-doc',3,'later-kb','file','SYNTHETIC_SYNC','synthetic-local','sync.txt','processing','enabled','feishu')").Error)
	rows, e := s.ListDocuments(a, "later-kb")
	require.NoError(t, e)
	require.Empty(t, rows)
	require.NoError(t, f.DB.Exec("INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,content_revision,is_enabled,chunk_index,start_at,end_at) VALUES('sync-chunk',3,'later-kb','sync-doc','SYNTHETIC_SYNC','text',1,true,0,0,14)").Error)
	require.NoError(t, f.DB.Exec("UPDATE knowledges SET parse_status='completed' WHERE id='sync-doc'").Error)
	rows, e = s.ReadDocument(a, "sync-doc")
	require.NoError(t, e)
	require.Len(t, rows, 1)
	require.NoError(t, f.DB.Exec("UPDATE chunks SET content='SYNTHETIC_SYNC_UPDATED' WHERE id='sync-chunk'").Error)
	rows, e = s.ReadDocument(a, "sync-doc")
	require.NoError(t, e)
	require.Equal(t, "SYNTHETIC_SYNC_UPDATED", rows[0].Text, "normal updates need no reapproval")
	require.NoError(t, f.DB.Exec("UPDATE tenant_portal_configs SET status='archived' WHERE tenant_id=3").Error)
	_, e = s.ReadDocument(a, "sync-doc")
	require.Error(t, e)
	require.NotContains(t, ids(), "later-kb")
	require.Contains(t, ids(), "private-home", "Portal withdrawal must not discard independent private authority")
	require.NoError(t, f.DB.Exec("UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=1").Error)
	require.Contains(t, ids(), "private-home")
}

func TestPortalM7PublishedExceptionsCASAndScope(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	s := publishedServiceM7(t, f)
	a := fixture.Key("A", 1, 1, true)
	targets, e := s.Resolve(a, []string{"public-kb"})
	require.NoError(t, e)
	require.Len(t, targets, 1)
	require.NoError(t, f.DB.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('owner',99,'owner','active')").Error)
	management := &portalService{db: f.DB, publicationExceptionConfig: PortalPolicyAuthorityConfig{Enabled: true, AllowSystemAdminReview: true, ContractRef: "ISOLATED"}}
	require.Error(t, management.SetPublicKnowledgeException(fixture.Web("A", 1), 99, "public-kb", true, 0, "not owner"))
	require.NoError(t, management.SetPublicKnowledgeException(fixture.Web("owner", 99), 99, "public-kb", true, 0, "synthetic sensitive exception"))
	require.Error(t, targets[0].Revalidate(a))
	_, e = s.ReadDocument(a, "public-doc")
	require.Error(t, e)
	require.ErrorIs(t, management.SetPublicKnowledgeException(fixture.Web("owner", 99), 99, "public-kb", false, 0, "stale allow"), types.ErrEnterprisePolicyConflict)
	var revision int64
	require.NoError(t, f.DB.Table("kb_publication_policies").Select("revision").Where("knowledge_base_id='public-kb'").Scan(&revision).Error)
	require.NoError(t, management.SetPublicKnowledgeException(fixture.Web("owner", 99), 99, "public-kb", false, revision, "restore Published default"))
	_, e = s.ReadDocument(a, "public-doc")
	require.NoError(t, e)
	_, e = s.Resolve(fixture.Key("A", 1, 3, false), []string{"public-kb"})
	require.Error(t, e, "old Key without enterprise capability cannot gain cross-space access")
	require.NoError(t, f.DB.Exec("UPDATE tenants SET status='inactive' WHERE id=99").Error)
	_, e = s.ReadDocument(a, "public-doc")
	require.Error(t, e)
	require.NoError(t, f.DB.Exec("UPDATE tenants SET status='active' WHERE id=99").Error)
	require.NoError(t, f.DB.Exec("UPDATE knowledge_bases SET deleted_at=CURRENT_TIMESTAMP WHERE id='public-kb'").Error)
	_, e = s.ReadDocument(a, "public-doc")
	require.Error(t, e, "historical Chunk cannot bypass deleted parent")
	_, e = s.ReadDocument(a, "private-home-doc")
	require.NoError(t, e)
	require.NoError(t, f.DB.Exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1").Error)
	_, e = s.ReadDocument(a, "private-home-doc")
	require.Error(t, e)
}

func TestPortalM7CompositionDefaultOff(t *testing.T) {
	require.Nil(t, NewPortalPersonalKnowledge(&config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil))
}
