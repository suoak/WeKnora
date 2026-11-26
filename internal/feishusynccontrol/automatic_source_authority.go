package feishusynccontrol

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AutomaticSourceKeyScopes is the existing repository's live, read-only scope
// lookup. Inject it at APP wiring to keep control independent of repository and
// connector packages (which themselves depend on the control admission API).
type AutomaticSourceKeyScopes interface {
	GetUserMCPTenantScope(context.Context, uint64, uint64) (*types.APIKeyTenantScope, error)
}

// Only DB-issued execution or ProcessSync's private pending proof may convert
// an ordinary call into background authority. TaskInitiator is audit data.
func automaticSourceAuthority(ctx context.Context, db, publication *gorm.DB, source, config string, keys ...AutomaticSourceKeyScopes) (NormalSourceAuthority, error) {
	if ctx == nil || db == nil {
		return NormalSourceAuthority{}, ErrRecoveryRejected
	}
	if execution, ok := types.SyncExecutionFromContext(ctx); ok {
		if automaticHTTPAuthority(ctx) {
			return NormalSourceAuthority{}, ErrRecoveryRejected
		}
		if execution.DataSourceID != source || execution.SyncLogID == "" || execution.TenantID == 0 || execution.Generation < 1 {
			return NormalSourceAuthority{}, ErrRecoveryRejected
		}
		if pending, ok := systemPendingExecutionFromContext(ctx); ok &&
			(pending.SourceID != source || pending.RunID != execution.SyncLogID || pending.TenantID != execution.TenantID) {
			return NormalSourceAuthority{}, ErrRecoveryRejected
		}
		return automaticResourceAuthority(ctx, db, publication, source, config, true, keys...)
	}
	if pending, ok := systemPendingExecutionFromContext(ctx); ok {
		if automaticHTTPAuthority(ctx) {
			return NormalSourceAuthority{}, ErrRecoveryRejected
		}
		if pending.SourceID != source || pending.RunID == "" || pending.TenantID == 0 {
			return NormalSourceAuthority{}, ErrRecoveryRejected
		}
		work := automaticAuthorityDB(ctx, db, publication)
		var count int64
		if err := work.Table("sync_logs").Where("id=? AND data_source_id=? AND tenant_id=? AND status='running' AND execution_claimed=false",
			pending.RunID, source, pending.TenantID).Count(&count).Error; err != nil {
			return NormalSourceAuthority{}, err
		}
		if count != 1 {
			return NormalSourceAuthority{}, ErrRecoveryRejected
		}
		out, err := automaticResourceAuthority(ctx, db, publication, source, config, true, keys...)
		if err == nil && out.TenantID != pending.TenantID {
			err = ErrRecoveryRejected
		}
		return out, err
	}
	// Require the explicit terminal identity installed by auth middleware; its
	// UserID fallback would turn restored audit-only task data into a session.
	principal, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if !ok || !principal.Valid() {
		return NormalSourceAuthority{}, ErrRecoveryRejected
	}
	caller := types.CallerFromContext(ctx)
	if _, key := types.TenantAPIKeyScopeFromContext(ctx); !key &&
		(principal.Type != types.PrincipalWebUser || principal.ID != caller.UserID) {
		return NormalSourceAuthority{}, ErrRecoveryRejected
	}
	return automaticResourceAuthority(ctx, db, publication, source, config, false, keys...)
}

// Controller/scheduler-only entry. Never accept an authenticated HTTP principal
// here. An audit-only TaskInitiator cannot choose the background policy actor.
func automaticBackgroundSourceAuthority(ctx context.Context, db, publication *gorm.DB, source, config string) (NormalSourceAuthority, error) {
	if ctx == nil || db == nil {
		return NormalSourceAuthority{}, ErrRecoveryRejected
	}
	if automaticHTTPAuthority(ctx) {
		return NormalSourceAuthority{}, ErrRecoveryRejected
	}
	return automaticResourceAuthority(ctx, db, publication, source, config, true)
}

func automaticHTTPAuthority(ctx context.Context) bool {
	if p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal); ok && p.Valid() {
		return true
	}
	_, ok := types.TenantAPIKeyScopeFromContext(ctx)
	return ok
}

func automaticAuthorizationContext(ctx context.Context) context.Context {
	ctx = types.WithCaller(ctx, types.Caller{})
	ctx = context.WithValue(ctx, types.UserIDContextKey, "")
	ctx = context.WithValue(ctx, types.UserContextKey, struct{}{})
	ctx = context.WithValue(ctx, types.PrincipalContextKey, types.Principal{})
	return context.WithValue(ctx, types.TenantAPIKeyScopeContextKey, struct{}{})
}

func automaticAuthorityDB(ctx context.Context, db, publication *gorm.DB) *gorm.DB {
	if publication != nil {
		return publication.WithContext(ctx)
	}
	return db.WithContext(ctx)
}

func automaticResourceAuthority(ctx context.Context, db, publication *gorm.DB, source, config string, background bool, keys ...AutomaticSourceKeyScopes) (NormalSourceAuthority, error) {
	authContext := ctx
	if background {
		authContext = automaticAuthorizationContext(ctx)
	}
	policyContext := authContext
	if key, ok := types.TenantAPIKeyScopeFromContext(authContext); ok && key.ScopeType != types.APIKeyScopeUserMCP {
		// api_platform:<id> and tenant-key display users are not human members.
		// Only the old DB policy projection uses current source management; the
		// actual request principal/key remains intact for resource authorization.
		policyContext = context.WithValue(policyContext, types.UserIDContextKey, "")
	}
	authority, err := NormalSourceFromDatabase(policyContext, db, publication, source, config)
	if err != nil {
		return authority, err
	}
	loader := automaticSourceFacts{db: automaticAuthorityDB(authContext, db, publication), authority: authority, lock: publication != nil}
	if len(keys) == 1 {
		loader.keys = keys[0]
	}
	target := access.SourceWriteTarget{TenantID: authority.TenantID, KnowledgeBaseID: authority.KBID, SourceID: source}
	if background {
		err = access.AuthorizeBackgroundSourceWrite(authContext, target, loader)
	} else {
		err = access.AuthorizeSourceWrite(authContext, target, loader)
	}
	if errors.Is(err, access.ErrForbidden) || errors.Is(err, access.ErrUnauthorized) || errors.Is(err, access.ErrNotFound) {
		err = errors.Join(ErrRecoveryRejected, err)
	}
	return authority, err
}

type automaticSourceFacts struct {
	keys      AutomaticSourceKeyScopes
	db        *gorm.DB
	authority NormalSourceAuthority
	lock      bool
}

func (l automaticSourceFacts) query(table string) *gorm.DB {
	q := l.db.Table(table)
	if l.lock {
		q = q.Clauses(clause.Locking{Strength: "SHARE"})
	}
	return q
}

func (l automaticSourceFacts) LoadSourceWriteFacts(ctx context.Context, target access.SourceWriteTarget, kind access.SourceWritePrincipal) (access.SourceWriteFacts, error) {
	var facts access.SourceWriteFacts
	var source types.DataSource
	if err := l.query("data_sources").Select("id,tenant_id,knowledge_base_id,type,status,legacy_access_state,config,deleted_at").
		Where("id=? AND deleted_at IS NULL", target.SourceID).Take(&source).Error; err != nil {
		return facts, err
	}
	fingerprint, err := feishusafety.SourceConfigFingerprint(source.ID, source.KnowledgeBaseID, source.Type,
		source.LegacyAccessState, source.TenantID, source.Config)
	if err != nil || fingerprint != l.authority.ConfigSHA256 || source.Status != "active" && source.Status != "error" {
		return facts, ErrRecoveryRejected
	}
	var tenant struct {
		ID     uint64
		Status string
	}
	if err := l.query("tenants").Select("id,status").Where("id=? AND deleted_at IS NULL", target.TenantID).Take(&tenant).Error; err != nil {
		return facts, err
	}
	var kb types.KnowledgeBase
	if err := l.query("knowledge_bases").Select("id,tenant_id,deleted_at").Where("id=?", target.KnowledgeBaseID).Take(&kb).Error; err != nil {
		return facts, err
	}
	var private struct{ OwnerID string }
	if err := l.query("feishu_private_kbs").Select("owner_id").Where("kb_id=?", kb.ID).Scan(&private).Error; err != nil {
		return facts, err
	}
	kb.PrivateOwnerID = private.OwnerID
	if kind != access.SourceWriteBackground && private.OwnerID != "" {
		// Machine-key display users are audit identities, not private owners.
		// User-MCP keeps its actual authenticated owner's WebUser principal.
		principal, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
		if !ok || principal.Type != types.PrincipalWebUser || principal.ID != private.OwnerID {
			return facts, ErrRecoveryRejected
		}
	}
	if err := l.db.Raw(`SELECT m.user_id,m.tenant_id,m.role,m.status,u.is_active AS user_active,
 (u.deleted_at IS NOT NULL) AS user_deleted,(m.deleted_at IS NOT NULL) AS deleted
 FROM tenant_members m JOIN users u ON u.id=m.user_id WHERE m.user_id=? AND m.tenant_id=? AND m.deleted_at IS NULL`,
		l.authority.ActorID, target.TenantID).Scan(&facts.Member).Error; err != nil {
		return facts, err
	}
	if facts.Member.Role != types.TenantRole(l.authority.ActorRole) {
		return facts, ErrRecoveryRejected
	}
	facts.TenantID, facts.TenantStatus, facts.KnowledgeBase = tenant.ID, tenant.Status, &kb
	facts.Source = &access.SourceWriteSource{ID: source.ID, TenantID: source.TenantID, KnowledgeBaseID: source.KnowledgeBaseID, Revision: fingerprint}
	if execution, ok := types.SyncExecutionFromContext(ctx); ok {
		var live types.SyncLog
		if err := l.query("sync_logs").Select("id,data_source_id,tenant_id,execution_generation").
			Where("id=? AND data_source_id=? AND tenant_id=? AND status='running' AND execution_claimed=true AND execution_generation=?",
				execution.SyncLogID, source.ID, source.TenantID, execution.Generation).Take(&live).Error; err != nil {
			return facts, err
		}
		facts.Execution = &types.SyncExecution{DataSourceID: live.DataSourceID, SyncLogID: live.ID, TenantID: live.TenantID, Generation: live.ExecutionGeneration}
	}
	if kind == access.SourceWriteBackground {
		config, err := source.ParseConfig()
		if err != nil {
			return facts, err
		}
		ref, err := types.ParseFeishuSourceReference(config)
		if err != nil {
			return facts, err
		}
		facts.Delegation = &access.SourceWriteDelegation{
			SourceID: source.ID, SourceRevision: fingerprint,
			ActorUserID: l.authority.ActorID, Mode: "application", Status: "active",
		}
		if ref != nil {
			facts.Delegation.Mode, facts.Delegation.PersonalOwnerID = "personal_ref", ref.OwnerUserID
		}
	}
	if kind == access.SourceWriteAPIKey {
		facts.Key, err = l.key(ctx, target.TenantID)
	}
	return facts, err
}

// Anonymous projection + Table query: no secret/hash column or TenantAPIKey
// AfterFind hook is used. Dynamic user-MCP scope is resolved by the repository.
func (l automaticSourceFacts) key(ctx context.Context, tenant uint64) (*access.SourceWriteKey, error) {
	scope, ok := types.TenantAPIKeyScopeFromContext(ctx)
	if !ok || scope.KeyID == 0 {
		return nil, ErrRecoveryRejected
	}
	var row struct {
		ID                             uint64
		TenantID                       *uint64
		OwnerUserID                    *string
		ScopeType                      types.APIKeyScopeType
		FullAccess                     bool
		KnowledgeBaseIDs, Capabilities types.StringArray
		ExpiresAt, RevokedAt           *time.Time
	}
	if err := l.query("tenant_api_keys").Select("id,tenant_id,owner_user_id,scope_type,full_access,knowledge_base_ids,capabilities,expires_at,revoked_at").Where("id=?", scope.KeyID).Take(&row).Error; err != nil {
		return nil, err
	}
	p, _ := ctx.Value(types.PrincipalContextKey).(types.Principal)
	current := types.TenantAPIKeyScope{
		KeyID: row.ID, ScopeType: row.ScopeType, FullAccess: row.FullAccess,
		KnowledgeBaseIDs: row.KnowledgeBaseIDs, Capabilities: row.Capabilities,
	}
	key := &access.SourceWriteKey{TenantID: tenant, Scope: current, Revoked: row.RevokedAt != nil, ExpiresAt: row.ExpiresAt}
	if row.OwnerUserID != nil {
		key.OwnerUserID = *row.OwnerUserID
	}
	switch row.ScopeType {
	case types.APIKeyScopeTenant:
		if row.TenantID == nil || *row.TenantID != tenant ||
			!(p.Type == types.PrincipalAPITenant && p.ID == strconv.FormatUint(tenant, 10) ||
				p.Type == types.PrincipalAPIExternalUser && strings.HasPrefix(p.ID, strconv.FormatUint(tenant, 10)+":") &&
					len(p.ID) > len(strconv.FormatUint(tenant, 10))+1) {
			return nil, ErrRecoveryRejected
		}
	case types.APIKeyScopePlatform:
		if row.TenantID != nil || p.Type != types.PrincipalAPIPlatform || p.ID != strconv.FormatUint(row.ID, 10) {
			return nil, ErrRecoveryRejected
		}
		key.Scope.FullAccess = false // Existing platform auth never derives full_access.
	case types.APIKeyScopeUserMCP:
		if row.FullAccess || p.Type != types.PrincipalWebUser || p.ID != key.OwnerUserID {
			return nil, ErrRecoveryRejected
		}
		if l.keys == nil {
			return nil, ErrNotReady
		}
		grant, err := l.keys.GetUserMCPTenantScope(ctx, row.ID, tenant)
		if err != nil {
			return nil, err
		}
		key.Scope.KnowledgeBaseIDs = nil
		key.Scope.KnowledgeBaseRestricted = grant.KBScopeMode == types.APIKeyKBScopeSelected
		if grant.KBScopeMode != types.APIKeyKBScopeAll && grant.KBScopeMode != types.APIKeyKBScopeSelected {
			return nil, ErrRecoveryRejected
		}
		for _, item := range grant.KnowledgeBases {
			key.Scope.KnowledgeBaseIDs = append(key.Scope.KnowledgeBaseIDs, item.KnowledgeBaseID)
		}
	default:
		return nil, ErrRecoveryRejected
	}
	if !p.Valid() || strings.TrimSpace(p.ID) != p.ID {
		return nil, ErrRecoveryRejected
	}
	return key, nil
}
