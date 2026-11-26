package feishusynccontrol

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

type automaticAuthorityFixture struct {
	db            *gorm.DB
	tenant        types.Tenant
	owner, viewer types.User
	source        types.DataSource
	config        string
}

func newAutomaticAuthorityFixture(t *testing.T) automaticAuthorityFixture {
	t.Helper()
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlog.Discard})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	f := automaticAuthorityFixture{
		db:     db,
		tenant: types.Tenant{Name: "automatic-source-test", Status: "active"},
	}
	require.NoError(t, db.Create(&f.tenant).Error)
	for _, seed := range []struct {
		user *types.User
		role types.TenantRole
	}{{&f.owner, types.TenantRoleOwner}, {&f.viewer, types.TenantRoleViewer}} {
		*seed.user = types.User{
			ID:           uuid.NewString(),
			Username:     uuid.NewString(),
			Email:        uuid.NewString() + "@example.invalid",
			PasswordHash: "synthetic",
			TenantID:     f.tenant.ID,
			IsActive:     true,
		}
		require.NoError(t, db.Create(seed.user).Error)
		require.NoError(
			t,
			db.Create(
				&types.TenantMember{
					UserID:   seed.user.ID,
					TenantID: f.tenant.ID,
					Role:     seed.role,
					Status:   types.TenantMemberStatusActive,
				},
			).Error,
		)
	}
	kb := types.KnowledgeBase{
		ID:        uuid.NewString(),
		TenantID:  f.tenant.ID,
		CreatorID: f.owner.ID,
		Name:      "source-write-test",
		Type:      "document",
	}
	require.NoError(t, db.Create(&kb).Error)
	f.source = types.DataSource{
		ID:                uuid.NewString(),
		TenantID:          f.tenant.ID,
		KnowledgeBaseID:   kb.ID,
		Type:              "feishu",
		Status:            "active",
		LegacyAccessState: "unmanaged",
		Config:            types.JSON(`{"resource_ids":["fixture-root"],"settings":{}}`),
	}
	require.NoError(t, db.Create(&f.source).Error)
	f.refreshConfig(t)
	return f
}

func (f *automaticAuthorityFixture) refreshConfig(t *testing.T) {
	t.Helper()
	var err error
	f.config, err = feishusafety.SourceConfigFingerprint(
		f.source.ID,
		f.source.KnowledgeBaseID,
		f.source.Type,
		f.source.LegacyAccessState,
		f.source.TenantID,
		f.source.Config,
	)
	require.NoError(t, err)
}

func (f automaticAuthorityFixture) userContext(user types.User) context.Context {
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, user.ID)
	ctx = types.WithCaller(
		ctx,
		types.Caller{TenantID: f.tenant.ID, UserID: user.ID, Role: types.TenantRoleViewer},
	)
	return types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: user.ID})
}

func TestAutomaticSourcePostgresCurrentResourceAuthority(t *testing.T) {
	f := newAutomaticAuthorityFixture(t)
	ctx := f.userContext(f.viewer)
	out, err := automaticSourceAuthority(
		ctx,
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.NoError(t, err, "own-space ordinary KB Viewer retains existing source policy")
	require.Equal(t, f.viewer.ID, out.ActorID)
	require.Equal(t, string(types.TenantRoleViewer), out.ActorRole)
	_, err = automaticSourceAuthority(
		types.WithExecutionTenant(context.Background(), f.tenant.ID),
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.Error(t, err, "execution tenant is not a session")
	audit := (types.TaskInitiator{UserID: f.viewer.ID, Role: types.TenantRoleOwner, APIKeyID: 999}).Apply(
		context.Background(),
	)
	_, err = automaticSourceAuthority(
		audit,
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.Error(t, err, "audit-only fallback principal is not HTTP authentication")
	out, err = automaticBackgroundSourceAuthority(audit, f.db, nil, f.source.ID, f.config)
	require.NoError(t, err)
	require.Equal(
		t,
		f.owner.ID,
		out.ActorID,
		"persistent current management, not TaskInitiator, supplies authority",
	)
	uid, _ := types.UserIDFromContext(audit)
	require.Equal(t, f.viewer.ID, uid, "authorization never rewrites original audit context")
	_, err = automaticBackgroundSourceAuthority(ctx, f.db, nil, f.source.ID, f.config)
	require.Error(t, err, "HTTP cannot switch itself to server background")
	require.NoError(
		t,
		f.db.Table("tenant_members").
			Where("user_id=? AND tenant_id=?", f.viewer.ID, f.tenant.ID).
			Update("status", "suspended").
			Error,
	)
	_, err = automaticSourceAuthority(
		ctx,
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.Error(t, err, "every call reloads member revocation")
	require.NoError(
		t,
		f.db.Table("tenant_members").
			Where("user_id=? AND tenant_id=?", f.viewer.ID, f.tenant.ID).
			Update("status", "active").
			Error,
	)
	require.NoError(
		t,
		f.db.Exec(
			"INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES(?,?)",
			f.source.KnowledgeBaseID,
			f.owner.ID,
		).Error,
	)
	_, err = automaticSourceAuthority(
		ctx,
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.Error(t, err, "new private owner takes effect immediately")
	_, err = automaticSourceAuthority(
		f.userContext(f.owner),
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.NoError(t, err)
	_, err = automaticSourceAuthority(
		types.WithCaller(
			ctx,
			types.Caller{
				TenantID: f.tenant.ID + 1000,
				UserID:   f.viewer.ID,
				Role:     types.TenantRoleOwner,
			},
		),
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.Error(t, err)
}

func TestAutomaticSourcePostgresClaimAndPendingIdentity(t *testing.T) {
	f := newAutomaticAuthorityFixture(t)
	log := types.SyncLog{
		ID:           uuid.NewString(),
		DataSourceID: f.source.ID,
		TenantID:     f.tenant.ID,
		Status:       types.SyncLogStatusRunning,
	}
	require.NoError(t, f.db.Create(&log).Error)
	pending := systemPendingExecution{SourceID: f.source.ID, RunID: log.ID, TenantID: f.tenant.ID}
	ctx := context.WithValue(context.Background(), systemPendingExecutionKey{}, pending)
	_, err := automaticSourceAuthority(
		ctx,
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.NoError(
		t,
		err,
		"private preflight proof is also rechecked against actual queued SQL run",
	)
	wrong := pending
	wrong.RunID = uuid.NewString()
	_, err = automaticSourceAuthority(
		context.WithValue(ctx, systemPendingExecutionKey{}, wrong),
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.Error(t, err)
	repo := repository.NewSyncLogRepository(f.db).(interfaces.SyncExecutionRepository)
	execution, claimed, err := repo.ClaimExecution(
		context.Background(),
		f.source.ID,
		log.ID,
		f.tenant.ID,
	)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = automaticSourceAuthority(
		ctx,
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.Error(t, err, "unclaimed proof cannot replace an actual claim")
	audit := (types.TaskInitiator{UserID: "payload-actor", Role: types.TenantRoleOwner}).Apply(
		context.Background(),
	)
	worker := types.WithSyncExecution(audit, execution)
	out, err := automaticSourceAuthority(
		worker,
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.NoError(t, err)
	require.Equal(t, f.owner.ID, out.ActorID)
	stale := execution
	stale.Generation++
	_, err = automaticSourceAuthority(
		types.WithSyncExecution(audit, stale),
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.Error(t, err)
	_, err = automaticSourceAuthority(
		types.WithSyncExecution(f.userContext(f.owner), execution),
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.Error(t, err, "an explicit HTTP principal cannot borrow a worker claim")
	require.NoError(
		t,
		f.db.Table("sync_logs").Where("id=?", log.ID).Update("status", "canceled").Error,
	)
	_, err = automaticSourceAuthority(
		worker,
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.Error(t, err)
}

func TestAutomaticSourcePostgresPersonalReferenceIsNotSpaceAdmin(t *testing.T) {
	f := newAutomaticAuthorityFixture(t)
	ref := types.FeishuSourceReference{
		Version:         1,
		OwnerUserID:     f.viewer.ID,
		Kind:            string(types.FeishuApplicationGrant),
		ConnectionID:    "fixture-reference",
		AppID:           "fixture-app",
		IdentityVersion: "fixture-version",
	}
	config := types.DataSourceConfig{
		ResourceIDs: []string{"fixture-root"},
		Settings:    map[string]interface{}{"credential_reference": ref},
	}
	raw, err := config.ToJSON()
	require.NoError(t, err)
	f.source.Config = raw
	require.NoError(t, f.db.Model(&f.source).Update("config", raw).Error)
	f.refreshConfig(t)
	out, err := automaticBackgroundSourceAuthority(
		context.Background(),
		f.db,
		nil,
		f.source.ID,
		f.config,
	)
	require.NoError(
		t,
		err,
		"personal application reference owner need not become a space administrator",
	)
	require.Equal(t, f.viewer.ID, out.ActorID)
	require.NoError(
		t,
		f.db.Table("users").Where("id=?", f.viewer.ID).Update("is_active", false).Error,
	)
	_, err = automaticBackgroundSourceAuthority(
		context.Background(),
		f.db,
		nil,
		f.source.ID,
		f.config,
	)
	require.Error(t, err)
	// This is local source-resource qualification, not a remote OAuth/ACL claim.
}

func (f automaticAuthorityFixture) rawKey(
	t *testing.T,
	mode types.APIKeyScopeType,
	full bool,
	capabilities types.StringArray,
	personalMode string,
) uint64 {
	t.Helper()
	var tenant, owner any
	if mode == types.APIKeyScopeTenant {
		tenant = f.tenant.ID
	}
	if mode == types.APIKeyScopeUserMCP {
		owner = f.owner.ID
	}
	caps, err := json.Marshal(capabilities)
	require.NoError(t, err)
	kbs, err := json.Marshal(types.StringArray{f.source.KnowledgeBaseID})
	require.NoError(t, err)
	var id uint64
	// Deliberately invalid encrypted secret proves no model AfterFind decrypt is
	// required by this adapter's public-field projection.
	require.NoError(
		t,
		f.db.Raw(
			`INSERT INTO tenant_api_keys(tenant_id,owner_user_id,scope_type,name,key_hash,api_key,
 full_access,capabilities,knowledge_base_ids,personal_scope_mode)
 VALUES(?,?,?,'resource-fixture',?,?,?,?::jsonb,?::jsonb,?) RETURNING id`,
			tenant,
			owner,
			mode,
			uuid.NewString(),
			utils.EncPrefix+"invalid-fixture-secret",
			full,
			string(caps),
			string(kbs),
			personalMode,
		).Scan(&id).Error,
	)
	return id
}

func (f automaticAuthorityFixture) keyContext(
	id uint64,
	mode types.APIKeyScopeType,
	full bool,
	capabilities types.StringArray,
	selected bool,
) context.Context {
	ctx := context.WithValue(
		context.Background(),
		types.UserIDContextKey,
		"system-"+strconv.FormatUint(f.tenant.ID, 10),
	)
	ctx = types.WithCaller(
		ctx,
		types.Caller{
			TenantID: f.tenant.ID,
			UserID:   "system-" + strconv.FormatUint(f.tenant.ID, 10),
			Role:     types.TenantRoleViewer,
		},
	)
	p := types.Principal{Type: types.PrincipalAPITenant, ID: strconv.FormatUint(f.tenant.ID, 10)}
	if mode == types.APIKeyScopePlatform {
		p = types.Principal{Type: types.PrincipalAPIPlatform, ID: strconv.FormatUint(id, 10)}
		ctx = context.WithValue(ctx, types.UserIDContextKey, p.StorageID())
		ctx = types.WithCaller(
			ctx,
			types.Caller{
				TenantID: f.tenant.ID,
				UserID:   p.StorageID(),
				Role:     types.TenantRoleViewer,
			},
		)
	}
	if mode == types.APIKeyScopeUserMCP {
		ctx = f.userContext(f.owner)
		p = types.Principal{Type: types.PrincipalWebUser, ID: f.owner.ID}
	}
	ctx = types.WithPrincipal(ctx, p)
	scope := types.TenantAPIKeyScope{
		KeyID:                   id,
		ScopeType:               mode,
		FullAccess:              full,
		Capabilities:            capabilities,
		KnowledgeBaseRestricted: selected,
	}
	if selected {
		scope.KnowledgeBaseIDs = types.StringArray{f.source.KnowledgeBaseID}
	}
	return types.WithTenantAPIKeyScope(ctx, scope)
}

func TestAutomaticSourcePostgresKeyIntersectionAndDynamicScope(t *testing.T) {
	f := newAutomaticAuthorityFixture(t)
	caps := types.StringArray{"manage_datasources", "ingest"}
	for _, mode := range []types.APIKeyScopeType{types.APIKeyScopeTenant, types.APIKeyScopePlatform} {
		id := f.rawKey(t, mode, false, caps, "fixed")
		ctx := f.keyContext(id, mode, false, caps, true)
		_, err := automaticSourceAuthority(
			ctx,
			f.db,
			nil,
			f.source.ID,
			f.config,
			repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
		)
		require.NoError(t, err)
		require.NoError(
			t,
			f.db.Table("tenant_api_keys").
				Where("id=?", id).
				Update("capabilities", types.StringArray{"manage_datasources"}).
				Error,
		)
		_, err = automaticSourceAuthority(
			ctx,
			f.db,
			nil,
			f.source.ID,
			f.config,
			repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
		)
		require.Error(t, err, "fresh ingest revocation cannot be masked by context")
	}
	id := f.rawKey(t, types.APIKeyScopeTenant, true, nil, "fixed")
	ctx := f.keyContext(id, types.APIKeyScopeTenant, true, nil, true)
	_, err := automaticSourceAuthority(
		ctx,
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.NoError(t, err)
	require.NoError(
		t,
		f.db.Table("tenant_api_keys").
			Where("id=?", id).
			Update("revoked_at", time.Now().UTC()).
			Error,
	)
	_, err = automaticSourceAuthority(
		ctx,
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.Error(t, err)
	for _, mode := range []string{"fixed", "dynamic_all"} {
		id := f.rawKey(t, types.APIKeyScopeUserMCP, false, caps, mode)
		if mode == "fixed" {
			scope := types.APIKeyTenantScope{
				APIKeyID:    id,
				TenantID:    f.tenant.ID,
				KBScopeMode: types.APIKeyKBScopeSelected,
			}
			require.NoError(t, f.db.Create(&scope).Error)
			require.NoError(
				t,
				f.db.Create(
					&types.APIKeyKnowledgeBaseScope{
						APIKeyTenantScopeID: scope.ID,
						APIKeyID:            id,
						TenantID:            f.tenant.ID,
						KnowledgeBaseID:     f.source.KnowledgeBaseID,
						SourceType:          types.APIKeyKBSourceOwned,
					},
				).Error,
			)
		}
		ctx := f.keyContext(id, types.APIKeyScopeUserMCP, false, caps, mode == "fixed")
		_, err := automaticSourceAuthority(
			ctx,
			f.db,
			nil,
			f.source.ID,
			f.config,
			repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
		)
		require.NoError(t, err)
		if mode == "fixed" {
			require.NoError(
				t,
				f.db.Where("api_key_id=?", id).Delete(&types.APIKeyKnowledgeBaseScope{}).Error,
			)
		} else {
			require.NoError(t, f.db.Table("tenant_members").
				Where("tenant_id=? AND user_id=?", f.tenant.ID, f.owner.ID).
				Update("status", "suspended").Error)
		}
		_, err = automaticSourceAuthority(
			ctx,
			f.db,
			nil,
			f.source.ID,
			f.config,
			repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
		)
		require.Error(t, err, "repository-resolved scope changes take effect")
		if mode == "dynamic_all" {
			require.NoError(
				t,
				f.db.Table("tenant_members").
					Where("tenant_id=? AND user_id=?", f.tenant.ID, f.owner.ID).
					Update("status", "active").
					Error,
			)
			require.Error(
				t,
				f.db.Table("tenant_api_keys").
					Where("id=?", id).
					Update("personal_scope_mode", "unknown").
					Error,
				"the real schema rejects an unknown mode without disabling its constraint",
			)
			_, err = automaticSourceAuthority(
				ctx,
				f.db,
				nil,
				f.source.ID,
				f.config,
				repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
			)
			require.NoError(
				t,
				err,
				"the rejected write preserves the existing legitimate mode and grant",
			)
		}
	}
	_, err = automaticSourceAuthority(
		context.WithValue(context.Background(), types.UserIDContextKey, f.owner.ID),
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.Error(t, err, "restored audit identity is not a real HTTP principal")
	require.NoError(
		t,
		f.db.Exec(
			"INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES(?,?)",
			f.source.KnowledgeBaseID,
			f.owner.ID,
		).Error,
	)
	for _, mode := range []types.APIKeyScopeType{types.APIKeyScopeTenant, types.APIKeyScopePlatform} {
		full := mode == types.APIKeyScopeTenant
		id := f.rawKey(t, mode, full, caps, "fixed")
		machine := f.keyContext(id, mode, full, caps, true)
		// Model attachTenantAPIKey's display user matching the actual owner.
		machine = context.WithValue(machine, types.UserIDContextKey, f.owner.ID)
		machine = types.WithCaller(
			machine,
			types.Caller{TenantID: f.tenant.ID, UserID: f.owner.ID, Role: types.TenantRoleOwner},
		)
		_, err := automaticSourceAuthority(
			machine,
			f.db,
			nil,
			f.source.ID,
			f.config,
			repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
		)
		require.Error(
			t,
			err,
			"a generic machine key cannot borrow its display user's private ownership",
		)
	}
	personal := f.rawKey(t, types.APIKeyScopeUserMCP, false, caps, "dynamic_all")
	_, err = automaticSourceAuthority(
		f.keyContext(personal, types.APIKeyScopeUserMCP, false, caps, false),
		f.db,
		nil,
		f.source.ID,
		f.config,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.NoError(t, err, "actual current private owner remains authorized through Personal MCP")
}
