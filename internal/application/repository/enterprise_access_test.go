package repository

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type syntheticEnterpriseDirectory struct{ active bool }

func (s *syntheticEnterpriseDirectory) IsEnterpriseEmployee(context.Context, string) (bool, error) {
	return s.active, nil
}

type syntheticPublicationAuthority struct{}

func (syntheticPublicationAuthority) AuthorizeEnterprisePolicyChange(_ context.Context, actor string, _ types.EnterprisePolicyTarget, action string) error {
	if action == "propose" && actor == "owner" || (action == "approve" || action == "revoke") && actor == "reviewer" {
		return nil
	}
	return access.ErrForbidden
}

type enterpriseFixture struct {
	db        *gorm.DB
	repo      *EnterpriseAccessRepository
	reader    *access.EnterprisePublicReader
	employees *syntheticEnterpriseDirectory
}

func enterpriseWebContext(user string) context.Context {
	return types.WithPrincipal(types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: user, Role: types.TenantRoleViewer}), types.Principal{Type: types.PrincipalWebUser, ID: user})
}

func enterpriseUserContext(user string, key uint64, public bool) context.Context {
	caps := types.StringArray{"retrieve"}
	if public {
		caps = append(caps, "enterprise_retrieve")
	}
	return types.WithTenantAPIKeyScope(enterpriseWebContext(user), types.TenantAPIKeyScope{KeyID: key, ScopeType: types.APIKeyScopeUserMCP, Capabilities: caps, KnowledgeBaseRestricted: true, KnowledgeBaseIDs: types.StringArray{"private-only"}})
}

func enterpriseMigration(t *testing.T, db *gorm.DB, direction string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "sqlite", "003034_enterprise_publication."+direction+".sql"))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(data)).Error)
}

func newEnterpriseFixture(t *testing.T) *enterpriseFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "synthetic.sqlite")+"?_foreign_keys=on&_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, q := range []string{
		`CREATE TABLE schema_migrations (version BIGINT PRIMARY KEY, dirty BOOLEAN NOT NULL)`,
		`INSERT INTO schema_migrations VALUES (3033,false)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY,is_active BOOLEAN NOT NULL,deleted_at DATETIME)`,
		`INSERT INTO users VALUES ('A',true,NULL),('B',true,NULL),('C',true,NULL),('owner',true,NULL),('reviewer',true,NULL)`,
		`CREATE TABLE tenants (id INTEGER PRIMARY KEY,status TEXT,updated_at DATETIME,deleted_at DATETIME)`,
		`INSERT INTO tenants VALUES (1,'active','2026-10-20 00:00:00',NULL),(99,'active','2026-10-20 00:00:00',NULL)`,
		`CREATE TABLE tenant_members (tenant_id INTEGER,user_id TEXT,status TEXT,role TEXT)`,
		`INSERT INTO tenant_members VALUES (99,'B','active','viewer')`,
		`CREATE TABLE tenant_portal_configs (tenant_id INTEGER PRIMARY KEY,status TEXT)`,
		`INSERT INTO tenant_portal_configs VALUES (99,'published')`,
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY,tenant_id INTEGER,type TEXT,is_temporary BOOLEAN,updated_at DATETIME,deleted_at DATETIME)`,
		`INSERT INTO knowledge_bases VALUES ('public-kb',99,'document',false,'2026-10-20 00:00:00',NULL),('unapproved',99,'document',false,'2026-10-20 00:00:00',NULL)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY,tenant_id INTEGER,knowledge_base_id TEXT,type TEXT,parse_status TEXT,enable_status TEXT,channel TEXT,updated_at DATETIME,deleted_at DATETIME)`,
		`INSERT INTO knowledges VALUES ('doc',99,'public-kb','file','completed','enabled','web','2026-10-20 00:00:00',NULL)`,
		`CREATE TABLE chunks (id TEXT PRIMARY KEY,tenant_id INTEGER,knowledge_base_id TEXT,knowledge_id TEXT,content TEXT,content_revision INTEGER,is_enabled BOOLEAN,chunk_type TEXT,chunk_index INTEGER,updated_at DATETIME,deleted_at DATETIME)`,
		`INSERT INTO chunks VALUES ('chunk',99,'public-kb','doc','Synthetic approved knowledge: ERR_M2_42',1,true,'text',0,'2026-10-20 00:00:00',NULL)`,
		`CREATE TABLE tenant_api_keys (id INTEGER PRIMARY KEY,owner_user_id TEXT,scope_type TEXT,capabilities TEXT,revoked_at DATETIME,expires_at DATETIME,full_access BOOLEAN)`,
		`INSERT INTO tenant_api_keys VALUES (1,'A','user_mcp','["retrieve","enterprise_retrieve"]',NULL,NULL,false),(2,'C','user_mcp','["retrieve","enterprise_retrieve"]',NULL,NULL,false),(3,'A','user_mcp','["retrieve"]',NULL,NULL,false),(4,'B','user_mcp','["retrieve","enterprise_retrieve"]',NULL,NULL,false)`,
	} {
		require.NoError(t, db.Exec(q).Error, q)
	}
	require.NoError(t, db.AutoMigrate(&types.AuditLog{}))
	enterpriseMigration(t, db, "up")
	documentMigration(t, db, "sqlite", "up")
	repo := NewEnterpriseAccessRepository(db)
	directory := &syntheticEnterpriseDirectory{active: true}
	return &enterpriseFixture{db: db, repo: repo, employees: directory, reader: access.NewEnterprisePublicReader(access.EnterprisePublicOptions{Enabled: true}, repo, directory, NewChunkRepository(db))}
}

func (f *enterpriseFixture) approve(t *testing.T, kind, id string) {
	t.Helper()
	target := types.EnterprisePolicyTarget{Kind: kind, ID: id}
	table, col, ok := target.Storage()
	require.True(t, ok)
	var row types.EnterprisePolicy
	require.NoError(t, f.db.Table(table).Where(col+"=?", id).Find(&row).Error)
	p, err := access.ChangeEnterprisePolicy(enterpriseWebContext("owner"), f.repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "propose", ExpectedRevision: row.Revision, Policy: "inherit", ContentClass: "ordinary", SourceACLState: "local", Reason: "synthetic clean text review"})
	require.NoError(t, err)
	_, err = access.ChangeEnterprisePolicy(enterpriseWebContext("reviewer"), f.repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "approve", ExpectedRevision: p.Revision, Reason: "independent synthetic approval"})
	require.NoError(t, err)
}

func (f *enterpriseFixture) approveClean(t *testing.T) {
	f.approve(t, "space", "99")
	f.approve(t, "kb", "public-kb")
	f.approve(t, "knowledge", "doc")
}

func TestEnterpriseM2ApprovedReadDoesNotGrantMembershipOrKBPrivilege(t *testing.T) {
	f := newEnterpriseFixture(t)
	f.approveClean(t)
	ctx := enterpriseUserContext("A", 1, true)
	before := types.CallerFromContext(ctx)
	snapshot, snapshotErr := f.repo.LoadEnterpriseReadSnapshot(ctx, "A", 1, "public-kb")
	require.NoError(t, snapshotErr)
	require.True(t, snapshot.UserActive)
	require.Equal(t, uint64(1), snapshot.Credential.ID)
	require.NotNil(t, snapshot.Credential.OwnerUserID)
	require.Equal(t, "A", *snapshot.Credential.OwnerUserID)
	require.Contains(t, snapshot.Credential.Capabilities, "enterprise_retrieve")
	grant, err := f.reader.Authorize(ctx, "public-kb")
	require.NoError(t, err)
	rows, err := f.reader.ReadApprovedChunks(ctx, grant, "doc")
	require.NoError(t, err)
	require.Equal(t, "Synthetic approved knowledge: ERR_M2_42", rows[0].Content)
	require.Equal(t, before, types.CallerFromContext(ctx))
	var count int64
	require.NoError(t, f.db.Table("tenant_members").Where("user_id=? AND tenant_id=?", "A", 99).Count(&count).Error)
	require.Zero(t, count)
	_, err = access.ResolveKB(ctx, access.KBRequest{Caller: before}, &types.KnowledgeBase{ID: "public-kb", TenantID: 99}, types.OrgRoleAdmin, nil, nil)
	require.Error(t, err)
	_, err = access.ResolveKB(ctx, access.KBRequest{Caller: before}, &types.KnowledgeBase{ID: "public-kb", TenantID: 99}, types.OrgRoleViewer, nil, nil)
	require.Error(t, err, "new grant cannot become a generic KB bypass")
	targets := grant.SearchTargets()
	require.Len(t, targets, 1)
	require.Equal(t, types.SearchTargetTypeKnowledge, targets[0].Type)
	require.Equal(t, uint64(99), targets[0].TenantID)
	require.Equal(t, []string{"doc"}, targets[0].KnowledgeIDs)
	_, err = f.reader.Authorize(enterpriseUserContext("B", 4, true), "public-kb")
	require.NoError(t, err)
}

func TestEnterpriseM2DefaultDenyAndOldCredentialCompatibility(t *testing.T) {
	f := newEnterpriseFixture(t)
	_, err := f.reader.Authorize(enterpriseUserContext("A", 1, true), "public-kb")
	require.Error(t, err, "published is not public")
	f.approveClean(t)
	for _, ctx := range []context.Context{enterpriseUserContext("A", 3, false), enterpriseUserContext("A", 999, true), enterpriseUserContext("C", 1, true), enterpriseWebContext("A"), context.Background()} {
		_, err = f.reader.Authorize(ctx, "public-kb")
		require.Error(t, err)
	}
	for _, kind := range []types.APIKeyScopeType{types.APIKeyScopeTenant, types.APIKeyScopePlatform} {
		ctx := types.WithTenantAPIKeyScope(enterpriseWebContext("A"), types.TenantAPIKeyScope{KeyID: 1, ScopeType: kind, FullAccess: true, Capabilities: types.StringArray{"retrieve", "enterprise_retrieve"}})
		_, err = f.reader.Authorize(ctx, "public-kb")
		require.Error(t, err)
	}
	ctx := types.WithPrincipal(enterpriseUserContext("A", 1, true), types.Principal{Type: types.PrincipalMCPEndpoint, ID: "99:endpoint"})
	_, err = f.reader.Authorize(ctx, "public-kb")
	require.Error(t, err)
	_, err = f.reader.Authorize(enterpriseUserContext("A", 1, true), "unapproved")
	require.Error(t, err)
	disabled := access.NewEnterprisePublicReader(access.EnterprisePublicOptions{}, f.repo, f.employees, NewChunkRepository(f.db))
	_, err = disabled.Authorize(enterpriseUserContext("A", 1, true), "public-kb")
	require.Error(t, err)
	var zero access.EnterprisePublicReader
	_, err = zero.Authorize(context.Background(), "public-kb")
	require.Error(t, err)
	var empty *access.EnterpriseReadGrant
	require.Empty(t, empty.SearchTargets())
	require.Empty(t, empty.KnowledgeIDs())
}

func TestEnterpriseM2RevocationAndChangedCredentialsAreLive(t *testing.T) {
	for _, q := range []string{
		`UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=99`,
		`UPDATE tenant_portal_configs SET status='archived' WHERE tenant_id=99`,
		`UPDATE tenants SET status='disabled' WHERE id=99`,
		`UPDATE knowledge_bases SET is_temporary=true WHERE id='public-kb'`,
		`UPDATE knowledge_bases SET deleted_at=CURRENT_TIMESTAMP WHERE id='public-kb'`,
		`UPDATE users SET is_active=false WHERE id='A'`,
		`UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1`,
		`UPDATE tenant_api_keys SET expires_at='2020-01-01' WHERE id=1`,
		`UPDATE tenant_api_keys SET capabilities='["retrieve"]' WHERE id=1`,
		`UPDATE tenant_api_keys SET owner_user_id='C' WHERE id=1`,
	} {
		t.Run(q, func(t *testing.T) {
			f := newEnterpriseFixture(t)
			f.approveClean(t)
			ctx := enterpriseUserContext("A", 1, true)
			grant, err := f.reader.Authorize(ctx, "public-kb")
			require.NoError(t, err)
			require.NoError(t, f.db.Exec(q).Error)
			_, err = f.reader.Authorize(ctx, "public-kb")
			require.Error(t, err)
			_, err = f.reader.ReadApprovedChunks(ctx, grant, "doc")
			require.Error(t, err)
		})
	}
}

func TestEnterpriseM2MixedUnknownExternalAndContentChangesHoldKB(t *testing.T) {
	for _, q := range []string{
		`UPDATE knowledge_access_policies SET policy='restricted',revision=revision+1 WHERE knowledge_id='doc'`,
		`UPDATE knowledge_access_policies SET policy='mcp_blocked',revision=revision+1 WHERE knowledge_id='doc'`,
		`UPDATE knowledge_access_policies SET policy='deny',revision=revision+1 WHERE knowledge_id='doc'`,
		`UPDATE knowledge_access_policies SET source_acl_state='unknown',revision=revision+1 WHERE knowledge_id='doc'`,
		`DELETE FROM knowledge_access_policies WHERE knowledge_id='doc'`,
		`UPDATE knowledges SET channel='feishu' WHERE id='doc'`,
		`UPDATE knowledges SET channel='gitlab' WHERE id='doc'`,
		`UPDATE knowledges SET parse_status='failed' WHERE id='doc'`,
		`UPDATE knowledges SET updated_at='2026-10-21' WHERE id='doc'`,
		`UPDATE chunks SET content_revision=content_revision+1,content='changed synthetic text' WHERE id='chunk'`,
		`INSERT INTO knowledges VALUES ('unreviewed',99,'public-kb','file','completed','enabled','web','2026-10-20 00:00:00',NULL)`,
	} {
		t.Run(q, func(t *testing.T) {
			f := newEnterpriseFixture(t)
			f.approveClean(t)
			_, err := f.reader.Authorize(enterpriseUserContext("C", 2, true), "public-kb")
			require.NoError(t, err)
			require.NoError(t, f.db.Exec(q).Error)
			_, err = f.reader.Authorize(enterpriseUserContext("C", 2, true), "public-kb")
			require.Error(t, err)
			_, err = f.reader.Authorize(enterpriseUserContext("B", 4, true), "public-kb")
			require.Error(t, err, "membership is not a public-policy bypass")
		})
	}
}

func TestEnterpriseM2ApprovalCASRevokeAndAuditAtomicity(t *testing.T) {
	f := newEnterpriseFixture(t)
	f.approveClean(t)
	target := types.EnterprisePolicyTarget{Kind: "kb", ID: "public-kb"}
	_, err := access.ChangeEnterprisePolicy(enterpriseWebContext("owner"), f.repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "approve", ExpectedRevision: 2, Reason: "self review"})
	require.Error(t, err)
	_, err = access.ChangeEnterprisePolicy(enterpriseWebContext("reviewer"), f.repo, nil, types.EnterprisePolicyChange{Target: target, Action: "revoke", Reason: "missing authority"})
	require.Error(t, err)
	_, err = access.ChangeEnterprisePolicy(enterpriseUserContext("A", 1, true), f.repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "revoke", Reason: "API key is not a reviewer"})
	require.Error(t, err)
	p, err := access.ChangeEnterprisePolicy(enterpriseWebContext("reviewer"), f.repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "revoke", ExpectedRevision: 0, Reason: "revoke wins over stale client"})
	require.NoError(t, err)
	require.Equal(t, int64(3), p.Revision)
	_, err = access.ChangeEnterprisePolicy(enterpriseWebContext("reviewer"), f.repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "approve", ExpectedRevision: 2, Reason: "stale approval"})
	require.ErrorIs(t, err, types.ErrEnterprisePolicyConflict)
	_, err = f.reader.Authorize(enterpriseUserContext("A", 1, true), "public-kb")
	require.Error(t, err)
	var audits []types.AuditLog
	require.NoError(t, f.db.Order("id").Find(&audits).Error)
	require.Len(t, audits, 7)
	require.Contains(t, string(audits[6].Details), `"revision":3`)
	require.NoError(t, f.db.Exec("DROP TABLE audit_logs").Error)
	_, err = access.ChangeEnterprisePolicy(enterpriseWebContext("owner"), f.repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "propose", ExpectedRevision: 3, Policy: "inherit", ContentClass: "ordinary", SourceACLState: "local", Reason: "must roll back"})
	require.Error(t, err)
	var row types.EnterprisePolicy
	require.NoError(t, f.db.Table("kb_publication_policies").Where("knowledge_base_id=?", "public-kb").Take(&row).Error)
	require.Equal(t, int64(3), row.Revision)
	require.Equal(t, "revoked", row.State)
}

func TestEnterpriseM2ConcurrentReadersAndRevocation(t *testing.T) {
	f := newEnterpriseFixture(t)
	f.approveClean(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = f.reader.Authorize(enterpriseUserContext("A", 1, true), "public-kb") }()
	}
	_, err := access.ChangeEnterprisePolicy(enterpriseWebContext("reviewer"), f.repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: types.EnterprisePolicyTarget{Kind: "kb", ID: "public-kb"}, Action: "revoke", Reason: "synthetic concurrent withdrawal"})
	require.NoError(t, err)
	wg.Wait()
	for i := 0; i < 5; i++ {
		_, err = f.reader.Authorize(enterpriseUserContext("A", 1, true), "public-kb")
		require.Error(t, err)
	}
}

func TestEnterpriseM2MigrationUpDownUpKeepsLegacy3033AndKeys(t *testing.T) {
	f := newEnterpriseFixture(t)
	for i := 0; i < 2; i++ {
		var count int64
		for _, table := range []string{"enterprise_space_policies", "kb_publication_policies", "knowledge_access_policies"} {
			require.NoError(t, f.db.Table(table).Count(&count).Error)
			require.Zero(t, count)
		}
		var legacy int
		require.NoError(t, f.db.Table("schema_migrations").Select("version").Scan(&legacy).Error)
		require.Equal(t, 3033, legacy)
		var capability string
		require.NoError(t, f.db.Table("tenant_api_keys").Select("capabilities").Where("id=3").Scan(&capability).Error)
		require.Equal(t, `["retrieve"]`, capability)
		require.NoError(t, f.db.Exec(`INSERT INTO kb_publication_policies(knowledge_base_id,updated_by) VALUES ('unapproved','owner')`).Error)
		var state types.EnterprisePolicy
		require.NoError(t, f.db.Table("kb_publication_policies").Where("knowledge_base_id='unapproved'").Take(&state).Error)
		require.Equal(t, "pending_review", state.State)
		require.Equal(t, "pending_review", state.Policy)
		require.False(t, state.InheritNewOrdinary)
		require.Error(t, f.db.Exec(`UPDATE kb_publication_policies SET state='approved' WHERE knowledge_base_id='unapproved'`).Error, "DB must reject incomplete approval")
		require.Error(t, f.db.Exec(`INSERT INTO knowledge_access_policies(knowledge_id,updated_by) VALUES ('missing','owner')`).Error, "FK must reject unknown resource")
		enterpriseMigration(t, f.db, "down")
		enterpriseMigration(t, f.db, "up")
	}
}

func TestEnterpriseM2NoAnonymousResourceLinksInParsedPilot(t *testing.T) {
	f := newEnterpriseFixture(t)
	require.NoError(t, f.db.Exec(`UPDATE chunks SET content='Synthetic https://example.invalid/file?sig=not-a-secret' WHERE id='chunk'`).Error)
	f.approveClean(t)
	ctx := enterpriseUserContext("A", 1, true)
	g, err := f.reader.Authorize(ctx, "public-kb")
	require.NoError(t, err)
	_, err = f.reader.ReadApprovedChunks(ctx, g, "doc")
	require.Error(t, err)
	_, err = f.reader.ReadApprovedChunks(ctx, g, "guessed-restricted-doc")
	require.Error(t, err)
}

// Runs against a task-owned Unix-socket PostgreSQL only. Never accepts a
// production DSN. The isolated Linux worker always sets this variable.
func TestEnterpriseM2PostgresCASAndMigration(t *testing.T) {
	dsn := os.Getenv("KNOWHUB_M2_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not configured; tracked separately from SQLite")
	}
	require.Contains(t, dsn, "dbname=knowhub_m2_test")
	require.Regexp(t, `host=/home/aqyfzx/ai/knowhub-mcp-m[234]-[0-9]{8}-[a-f0-9]{6}/pgsocket-[0-9]+ `, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	schema := "m2_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	require.NoError(t, db.Exec("CREATE SCHEMA "+schema).Error)
	// Open a pool with a per-connection schema, so approve/revoke actually
	// overlap on separate PostgreSQL transactions rather than one connection.
	db, err = gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	policyPool, err := db.DB()
	require.NoError(t, err)
	policyPool.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = policyPool.Close() })
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA " + schema + " CASCADE").Error })
	for _, q := range []string{
		`CREATE TABLE users(id VARCHAR(36) PRIMARY KEY,is_active BOOLEAN NOT NULL,deleted_at TIMESTAMPTZ)`,
		`INSERT INTO users VALUES ('owner',true,NULL),('reviewer',true,NULL)`,
		`CREATE TABLE tenants(id INTEGER PRIMARY KEY, status TEXT, updated_at TIMESTAMPTZ, deleted_at TIMESTAMPTZ)`,
		`INSERT INTO tenants VALUES (99,'active','2026-10-20',NULL)`,
		`CREATE TABLE knowledge_bases(id VARCHAR(36) PRIMARY KEY,tenant_id INTEGER,is_temporary BOOLEAN,updated_at TIMESTAMPTZ,deleted_at TIMESTAMPTZ)`,
		`CREATE TABLE knowledges(id VARCHAR(36) PRIMARY KEY,knowledge_base_id VARCHAR(36),tenant_id INTEGER,updated_at TIMESTAMPTZ,deleted_at TIMESTAMPTZ)`,
	} {
		require.NoError(t, db.Exec(q).Error)
	}
	require.NoError(t, db.AutoMigrate(&types.AuditLog{}))
	up, err := os.ReadFile("../../../migrations/versioned/003034_enterprise_publication.up.sql")
	require.NoError(t, err)
	down, err := os.ReadFile("../../../migrations/versioned/003034_enterprise_publication.down.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Exec(string(up)).Error)
		require.NoError(t, db.Exec(string(down)).Error)
	}
	require.NoError(t, db.Exec(string(up)).Error)
	repo := NewEnterpriseAccessRepository(db)
	target := types.EnterprisePolicyTarget{Kind: "space", ID: "99"}
	p, err := access.ChangeEnterprisePolicy(enterpriseWebContext("owner"), repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "propose", Policy: "inherit", ContentClass: "ordinary", SourceACLState: "local", Reason: "PostgreSQL synthetic scope"})
	require.NoError(t, err)
	var wg sync.WaitGroup
	errorsFound := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, e := access.ChangeEnterprisePolicy(enterpriseWebContext("reviewer"), repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "approve", ExpectedRevision: p.Revision, Reason: "concurrent review"})
		errorsFound <- e
	}()
	go func() {
		defer wg.Done()
		_, e := access.ChangeEnterprisePolicy(enterpriseWebContext("reviewer"), repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "revoke", ExpectedRevision: 0, Reason: "withdraw wins"})
		errorsFound <- e
	}()
	wg.Wait()
	close(errorsFound)
	for e := range errorsFound {
		if e != nil {
			require.ErrorIs(t, e, types.ErrEnterprisePolicyConflict)
		}
	}
	var result types.EnterprisePolicy
	require.NoError(t, db.Table("enterprise_space_policies").Where("tenant_id=99").Take(&result).Error)
	require.Equal(t, "revoked", result.State)
	_, err = access.ChangeEnterprisePolicy(enterpriseWebContext("reviewer"), repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "approve", ExpectedRevision: p.Revision, Reason: "stale approval must fail"})
	require.ErrorIs(t, err, types.ErrEnterprisePolicyConflict)
	// Also exercise real JSONB credential projection and read grants against
	// PostgreSQL, not just migration syntax and CAS. All content is synthetic.
	for _, q := range []string{
		`INSERT INTO users VALUES ('A',true,NULL)`,
		`ALTER TABLE knowledge_bases ADD COLUMN type TEXT NOT NULL DEFAULT 'document'`,
		`ALTER TABLE knowledges ADD COLUMN type TEXT NOT NULL DEFAULT 'file'`,
		`ALTER TABLE knowledges ADD COLUMN parse_status TEXT NOT NULL DEFAULT 'completed'`,
		`ALTER TABLE knowledges ADD COLUMN enable_status TEXT NOT NULL DEFAULT 'enabled'`,
		`ALTER TABLE knowledges ADD COLUMN channel TEXT NOT NULL DEFAULT 'web'`,
		`INSERT INTO knowledge_bases VALUES ('public-kb',99,false,'2026-10-20',NULL,'document')`,
		`INSERT INTO knowledges VALUES ('doc','public-kb',99,'2026-10-20',NULL,'file','completed','enabled','web')`,
		`CREATE TABLE chunks(id TEXT PRIMARY KEY,tenant_id INTEGER,knowledge_base_id TEXT,knowledge_id TEXT,content TEXT,content_revision INTEGER,is_enabled BOOLEAN,chunk_type TEXT,chunk_index INTEGER,updated_at TIMESTAMPTZ,deleted_at TIMESTAMPTZ)`,
		`INSERT INTO chunks VALUES ('chunk',99,'public-kb','doc','Synthetic PostgreSQL public text',1,true,'text',0,'2026-10-20',NULL)`,
		`CREATE TABLE tenant_api_keys(id BIGINT PRIMARY KEY,owner_user_id TEXT,scope_type TEXT,capabilities JSONB,revoked_at TIMESTAMPTZ,expires_at TIMESTAMPTZ,full_access BOOLEAN)`,
		`INSERT INTO tenant_api_keys VALUES(1,'A','user_mcp','["retrieve","enterprise_retrieve"]',NULL,NULL,false)`,
		`CREATE TABLE tenant_portal_configs(tenant_id INTEGER PRIMARY KEY,status TEXT)`,
		`INSERT INTO tenant_portal_configs VALUES(99,'published')`,
	} {
		require.NoError(t, db.Exec(q).Error)
	}
	documentMigration(t, db, "versioned", "up")
	directory := &syntheticEnterpriseDirectory{active: true}
	f := &enterpriseFixture{db: db, repo: repo, employees: directory, reader: access.NewEnterprisePublicReader(access.EnterprisePublicOptions{Enabled: true}, repo, directory, NewChunkRepository(db))}
	f.approveClean(t)
	ctx := enterpriseUserContext("A", 1, true)
	grant, err := f.reader.Authorize(ctx, "public-kb")
	require.NoError(t, err)
	content, err := f.reader.ReadApprovedChunks(ctx, grant, "doc")
	require.NoError(t, err)
	require.Equal(t, "Synthetic PostgreSQL public text", content[0].Content)
	require.NoError(t, db.Exec(`UPDATE tenant_portal_configs SET status='archived' WHERE tenant_id=99`).Error)
	_, err = f.reader.ReadApprovedChunks(ctx, grant, "doc")
	require.Error(t, err)
	require.NoError(t, db.Exec(`UPDATE tenant_portal_configs SET status='published' WHERE tenant_id=99`).Error)
	// M3: real PostgreSQL row/TRUNCATE triggers, exact subject grant CAS and
	// migration incarnation reset; no timestamp/revision cooperation required.
	version, _, err := resourceVersion(db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: "doc"})
	require.NoError(t, err)
	explicit, err := access.ChangeDocumentSubjectGrant(enterpriseWebContext("reviewer"), repo, syntheticDocumentAuthority{}, types.DocumentGrantChange{DocumentSubjectGrant: types.DocumentSubjectGrant{KnowledgeID: "doc", UserID: "A", Operation: types.DocumentRead, State: "approved", ResourceVersion: version}, Reason: "PostgreSQL synthetic document entitlement"})
	require.NoError(t, err)
	_, err = access.ChangeDocumentSubjectGrant(enterpriseWebContext("reviewer"), repo, syntheticDocumentAuthority{}, types.DocumentGrantChange{DocumentSubjectGrant: types.DocumentSubjectGrant{KnowledgeID: "doc", UserID: "A", Operation: types.DocumentRead, State: "revoked"}, Reason: "revoke wins stale client"})
	require.NoError(t, err)
	_, err = access.ChangeDocumentSubjectGrant(enterpriseWebContext("reviewer"), repo, syntheticDocumentAuthority{}, types.DocumentGrantChange{DocumentSubjectGrant: types.DocumentSubjectGrant{KnowledgeID: "doc", UserID: "A", Operation: types.DocumentRead, State: "approved", Revision: explicit.Revision, ResourceVersion: version}, Reason: "stale approval"})
	require.ErrorIs(t, err, types.ErrEnterprisePolicyConflict)
	for attempt := 0; attempt < 20; attempt++ {
		require.NoError(t, db.Exec(`DELETE FROM document_subject_grants WHERE knowledge_id='doc' AND user_id='A' AND operation='read'`).Error)
		start := make(chan struct{})
		results := make(chan struct {
			state string
			err   error
		}, 2)
		for _, state := range []string{"approved", "revoked"} {
			go func(state string) {
				<-start
				_, e := access.ChangeDocumentSubjectGrant(enterpriseWebContext("reviewer"), repo, syntheticDocumentAuthority{}, types.DocumentGrantChange{DocumentSubjectGrant: types.DocumentSubjectGrant{KnowledgeID: "doc", UserID: "A", Operation: types.DocumentRead, State: state, ResourceVersion: version}, Reason: "race first approval against withdrawal"})
				results <- struct {
					state string
					err   error
				}{state, e}
			}(state)
		}
		close(start)
		for i := 0; i < 2; i++ {
			outcome := <-results
			if outcome.state == "revoked" {
				require.NoError(t, outcome.err)
			} else if outcome.err != nil {
				require.ErrorIs(t, outcome.err, types.ErrEnterprisePolicyConflict)
			}
		}
		last, e := repo.LoadDocumentSubjectGrant(context.Background(), "doc", "A", types.DocumentRead)
		require.NoError(t, e)
		require.Equal(t, "revoked", last.State, "withdrawal must win the first-row race")
	}
	require.NoError(t, db.Exec(`CREATE TABLE tenant_members(tenant_id INTEGER,user_id TEXT,status TEXT,role TEXT)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO tenant_members VALUES(99,'A','active','viewer')`).Error)
	f.documentPolicy(t, "doc", "restricted")
	last, err := repo.LoadDocumentSubjectGrant(context.Background(), "doc", "A", types.DocumentRead)
	require.NoError(t, err)
	_, err = access.ChangeDocumentSubjectGrant(enterpriseWebContext("reviewer"), repo, syntheticDocumentAuthority{}, types.DocumentGrantChange{DocumentSubjectGrant: types.DocumentSubjectGrant{KnowledgeID: "doc", UserID: "A", Operation: types.DocumentRead, State: "approved", Revision: last.Revision, ResourceVersion: version}, Reason: "synthetic PostgreSQL private read"})
	require.NoError(t, err)
	privateReader := f.documentReader()
	privateCtx := documentCtx("A", 1)
	privateGrant, err := privateReader.Authorize(privateCtx, "public-kb", "doc", types.DocumentRead)
	require.NoError(t, err)
	privateChunks, err := privateReader.ReadText(privateCtx, privateGrant, NewChunkRepository(db))
	require.NoError(t, err)
	require.Equal(t, "Synthetic PostgreSQL public text", privateChunks[0].Content)
	require.NoError(t, db.Exec(`UPDATE chunks SET content='Synthetic changed without timestamp' WHERE id='chunk'`).Error)
	require.Error(t, privateGrant.Revalidate(privateCtx))
	changed, _, err := resourceVersion(db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: "doc"})
	require.NoError(t, err)
	require.NotEqual(t, version, changed)
	require.Error(t, f.reader.Revalidate(ctx, grant))
	require.NoError(t, db.Exec(`TRUNCATE chunks`).Error)
	truncated, _, err := resourceVersion(db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: "doc"})
	require.NoError(t, err)
	require.NotEqual(t, changed, truncated)
	documentMigration(t, db, "versioned", "down")
	documentMigration(t, db, "versioned", "up")
	reinstalled, _, err := resourceVersion(db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: "doc"})
	require.NoError(t, err)
	require.NotEqual(t, truncated, reinstalled)
	_, err = f.reader.Authorize(ctx, "public-kb")
	require.Error(t, err)
}
