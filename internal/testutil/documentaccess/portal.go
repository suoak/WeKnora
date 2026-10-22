package documentaccess

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// Portal builds three entirely synthetic targets: clean public KBs in two
// tenants, independent private-home, plus mixed/unknown resources to exclude.
func (f *Fixture) Portal(t *testing.T) *access.PortalMCPResolver {
	t.Helper()
	exec := func(q string, args ...any) { require.NoError(t, f.DB.Exec(q, args...).Error) }
	exec("UPDATE knowledges SET knowledge_base_id='unknown-kb' WHERE id='denied-doc'")
	exec("UPDATE chunks SET knowledge_base_id='unknown-kb' WHERE knowledge_id='denied-doc'")
	exec("INSERT INTO tenant_portal_configs(tenant_id,status,display_name,created_by,updated_by) VALUES(1,'published','synthetic-one','owner','owner')")
	for _, kb := range []string{"public-two", "private-home"} {
		exec("INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,type) VALUES(?,?,1,?,'synthetic','document')", kb, kb, "model-"+kb)
		exec("INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,file_name,parse_status,enable_status,channel) VALUES(?,1,?,'file',?,'synthetic-local',?,'completed','enabled','web')", kb+"-doc", kb, PublicText+"-"+kb, kb+".txt")
		exec("INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,content_revision,is_enabled,chunk_index,start_at,end_at) VALUES(?,1,?,?,?,'text',1,true,0,0,100)", kb+"-chunk", kb, kb+"-doc", PublicText+"-"+kb)
	}
	f.Approve(t, "space", "1", "inherit", "ordinary")
	f.Approve(t, "space", "99", "inherit", "ordinary")
	f.Approve(t, "kb", "public-kb", "inherit", "ordinary")
	f.Approve(t, "kb", "public-two", "inherit", "ordinary")
	f.Approve(t, "knowledge", "public-doc", "inherit", "ordinary")
	f.Approve(t, "knowledge", "public-two-doc", "inherit", "ordinary")
	f.Approve(t, "knowledge", "private-home-doc", "inherit", "ordinary")
	// Synthetic private-home is not public: it deliberately has no KB approval.
	base := &access.LiveDocumentBaseVerifier{Members: fixtureMembership{repository.NewTenantMemberRepository(f.DB)}, Keys: repository.NewTenantAPIKeyRepository(f.DB).(interfaces.UserMCPAPIKeyRepository), KBs: repository.NewKnowledgeBaseRepository(f.DB), EnterpriseScope: func(_ context.Context, tenant uint64, kb string) error {
		if tenant == 99 && kb == "public-kb" || tenant == 1 && kb == "public-two" {
			return nil
		}
		return access.ErrForbidden
	}}
	accounts := &repository.KnowHubPersonalUserVerifier{DB: f.DB}
	docs := access.NewDocumentReader(true, f.Store, base, accounts)
	chunks := repository.NewChunkRepository(f.DB)
	public := access.NewEnterprisePublicReader(access.EnterprisePublicOptions{Enabled: true}, f.Store, accounts, chunks)
	return &access.PortalMCPResolver{Enabled: true, Directory: f.Store, Store: f.Store, Public: public, Documents: docs, Employees: accounts}
}
