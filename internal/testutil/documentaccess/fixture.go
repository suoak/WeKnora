// Package documentaccess supplies only synthetic, temporary-database fixtures.
package documentaccess

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const PublicText = "PUBLIC_M5_APPROVED"
const Canary = "SECRET_M5_SENSITIVE_CANARY"

type Fixture struct {
	DB     *gorm.DB
	Store  *repository.EnterpriseAccessRepository
	Reader *access.DocumentReader
}
type employees struct{}

func (employees) IsEnterpriseEmployee(_ context.Context, user string) (bool, error) {
	return user == "A" || user == "B" || user == "C" || user == "D", nil
}

type authority struct{}

func (authority) AuthorizeEnterprisePolicyChange(_ context.Context, actor string, _ types.EnterprisePolicyTarget, action string) error {
	if actor == "owner" && action == "propose" || actor == "reviewer" && (action == "approve" || action == "revoke") {
		return nil
	}
	return access.ErrForbidden
}
func (authority) AuthorizeDocumentGrantChange(_ context.Context, actor string, _ types.DocumentGrantChange) error {
	if actor == "reviewer" {
		return nil
	}
	return access.ErrForbidden
}
func Web(user string, tenant uint64) context.Context {
	ctx := types.WithPrincipal(types.WithCaller(context.Background(), types.Caller{TenantID: tenant, UserID: user, Role: types.TenantRoleViewer}), types.Principal{Type: types.PrincipalWebUser, ID: user})
	return types.WithExecutionTenant(ctx, tenant)
}
func Key(user string, tenant, key uint64, enterprise bool) context.Context {
	caps := types.StringArray{"retrieve"}
	if enterprise {
		caps = append(caps, "enterprise_retrieve")
	}
	return types.WithTenantAPIKeyScope(Web(user, tenant), types.TenantAPIKeyScope{KeyID: key, ScopeType: types.APIKeyScopeUserMCP, Capabilities: caps})
}
func New(t *testing.T) *Fixture {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	root := cwd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		require.NotEqual(t, parent, root)
		root = parent
	}
	path := filepath.Join(t.TempDir(), "business.sqlite")
	require.NoError(t, os.Chdir(root))
	migrationErr := database.RunMigrationsWithOptions("sqlite3://unused", database.MigrationOptions{SQLiteDBPath: path})
	require.NoError(t, os.Chdir(cwd))
	require.NoError(t, migrationErr)
	db, err := gorm.Open(sqlite.Open(path+"?_foreign_keys=on&_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	return Seed(t, db)
}

// Seed uses the same canonical fixtures on a formally migrated isolated DB.
// Callers own the database lifecycle; no production connection is accepted here.
func Seed(t *testing.T, db *gorm.DB) *Fixture {
	t.Helper()
	exec := func(query string, args ...any) { require.NoError(t, db.Exec(query, args...).Error) }
	for _, tenant := range []uint64{1, 99} {
		exec("INSERT INTO tenants(id,name,business) VALUES(?,?,'synthetic')", tenant, "synthetic-"+strconv.FormatUint(tenant, 10))
	}
	for _, user := range []string{"A", "B", "C", "D", "owner", "reviewer"} {
		home := uint64(99)
		if user == "A" || user == "owner" || user == "reviewer" {
			home = 1
		}
		exec("INSERT INTO users(id,username,email,password_hash,tenant_id) VALUES(?,?,?,'synthetic-not-password',?)", user, user, user+"@example.invalid", home)
		exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES(?,?,'viewer','active')", user, home)
	}
	for _, kb := range []string{"public-kb", "private-kb", "unknown-kb"} {
		exec("INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,type) VALUES(?,?,99,'synthetic','synthetic','document')", kb, kb)
	}
	exec("INSERT INTO tenant_portal_configs(tenant_id,status,display_name,created_by,updated_by) VALUES(99,'published','synthetic','owner','owner')")
	for _, doc := range []struct{ id, kb, text string }{{"public-doc", "public-kb", PublicText}, {"denied-doc", "public-kb", Canary}, {"sensitive", "private-kb", Canary}, {"other-sensitive", "private-kb", Canary}, {"blocked", "private-kb", Canary}, {"unknown-doc", "unknown-kb", Canary}} {
		exec("INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,file_name,parse_status,enable_status,channel) VALUES(?,99,?,'file',?,'synthetic-local',?,'completed','enabled','web')", doc.id, doc.kb, doc.text, doc.id+".txt")
		exec("INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,content_revision,is_enabled,chunk_index,start_at,end_at) VALUES(?,99,?,?,?,'text',1,true,0,0,100)", doc.id+"-chunk", doc.kb, doc.id, doc.text)
	}
	for _, key := range []struct {
		id     int
		user   string
		tenant uint64
		caps   string
		full   bool
	}{{1, "A", 1, `["retrieve","enterprise_retrieve"]`, false}, {2, "C", 99, `["retrieve"]`, false}, {3, "A", 1, `["retrieve"]`, false}, {4, "D", 99, `["retrieve"]`, false}, {6, "B", 99, `["retrieve"]`, false}, {7, "D", 99, `["retrieve"]`, false}, {8, "D", 99, `["retrieve"]`, false}} {
		exec("INSERT INTO tenant_api_keys(id,tenant_id,owner_user_id,scope_type,name,key_hash,api_key,capabilities,full_access) VALUES(?,NULL,?,'user_mcp',?,?,?, ?,?)", key.id, key.user, "synthetic-"+strconv.Itoa(key.id), "synthetic-hash-"+strconv.Itoa(key.id), "not-a-usable-key-"+strconv.Itoa(key.id), key.caps, key.full)
		mode := "all"
		if key.id == 7 {
			mode = "selected"
		}
		exec("INSERT INTO api_key_tenant_scopes(api_key_id,tenant_id,kb_scope_mode) VALUES(?,?,?)", key.id, key.tenant, mode)
	}
	exec("INSERT INTO api_key_kb_scopes(api_key_tenant_scope_id,api_key_id,tenant_id,knowledge_base_id,source_type) SELECT id,7,99,'public-kb','owned' FROM api_key_tenant_scopes WHERE api_key_id=7")
	exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=8")
	store := repository.NewEnterpriseAccessRepository(db)
	base := &access.LiveDocumentBaseVerifier{Members: fixtureMembership{repository.NewTenantMemberRepository(db)}, Keys: repository.NewTenantAPIKeyRepository(db).(interfaces.UserMCPAPIKeyRepository), KBs: repository.NewKnowledgeBaseRepository(db), EnterpriseScope: func(_ context.Context, tenant uint64, kb string) error {
		if tenant == 99 && kb == "public-kb" {
			return nil
		}
		return access.ErrForbidden
	}}
	f := &Fixture{DB: db, Store: store, Reader: access.NewDocumentReader(true, store, base, employees{})}
	f.Approve(t, "space", "99", "inherit", "ordinary")
	f.Approve(t, "kb", "public-kb", "inherit", "ordinary")
	f.Approve(t, "knowledge", "public-doc", "inherit", "ordinary")
	f.Approve(t, "knowledge", "denied-doc", "deny", "ordinary")
	for _, doc := range []string{"sensitive", "other-sensitive", "blocked"} {
		policy := "restricted"
		if doc == "blocked" {
			policy = "mcp_blocked"
		}
		f.Approve(t, "knowledge", doc, policy, "mixed")
	}
	f.Subject(t, "sensitive", "D", "approved", 0)
	f.Subject(t, "blocked", "D", "approved", 0)
	var version int
	var dirty bool
	require.NoError(t, db.Raw("SELECT version,dirty FROM schema_migrations").Row().Scan(&version, &dirty))
	require.Equal(t, 3037, version)
	require.False(t, dirty)
	t.Log("synthetic real migration state: version=3037 dirty=false")
	return f
}
func (f *Fixture) Approve(t *testing.T, kind, id, policy, class string) {
	t.Helper()
	target := types.EnterprisePolicyTarget{Kind: kind, ID: id}
	table, col, ok := target.Storage()
	require.True(t, ok)
	var prior types.EnterprisePolicy
	require.NoError(t, f.DB.Table(table).Where(col+"=?", id).Find(&prior).Error)
	proposed, err := access.ChangeEnterprisePolicy(Web("owner", 1), f.Store, authority{}, types.EnterprisePolicyChange{Target: target, Action: "propose", ExpectedRevision: prior.Revision, Policy: policy, ContentClass: class, SourceACLState: "local", Reason: "synthetic reviewed business fixture"})
	require.NoError(t, err)
	_, err = access.ChangeEnterprisePolicy(Web("reviewer", 1), f.Store, authority{}, types.EnterprisePolicyChange{Target: target, Action: "approve", ExpectedRevision: proposed.Revision, Reason: "synthetic independent approval"})
	require.NoError(t, err)
}
func (f *Fixture) Subject(t *testing.T, doc, user, state string, revision int64) *types.DocumentSubjectGrant {
	t.Helper()
	snapshot, err := f.Store.LoadDocumentReadSnapshot(context.Background(), user, 0, "private-kb")
	require.NoError(t, err)
	version := ""
	for _, d := range snapshot.Documents {
		if d.ID == doc {
			version = d.CurrentVersion
		}
	}
	require.NotEmpty(t, version)
	grant, err := access.ChangeDocumentSubjectGrant(Web("reviewer", 1), f.Store, authority{}, types.DocumentGrantChange{DocumentSubjectGrant: types.DocumentSubjectGrant{KnowledgeID: doc, UserID: user, Operation: types.DocumentRead, State: state, Revision: revision, ResourceVersion: version}, Reason: "synthetic precise document grant"})
	require.NoError(t, err)
	return grant
}

type fixtureMembership struct {
	repo interfaces.TenantMemberRepository
}

func (m fixtureMembership) GetMembership(ctx context.Context, user string, tenant uint64) (*types.TenantMember, error) {
	return m.repo.Get(ctx, user, tenant)
}
