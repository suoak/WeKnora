package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const sourceRebindPrefix = "integration.feishu.rebind."

// ErrFeishuRebindConflict rejects a proposal whose protected snapshot changed.
var ErrFeishuRebindConflict = errors.New("feishu_source_rebind_snapshot_changed")

func requireRebindOperator(ctx context.Context, tx *gorm.DB, operator string) error {
	actor, ok := types.UserIDFromContext(ctx)
	if !ok || actor != operator || !types.IsSystemAdminFromContext(ctx) {
		return access.ErrForbidden
	}
	var user types.User
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND is_active = ? AND is_system_admin = ?", operator, true, true).
		First(&user).
		Error; err != nil {
		return access.ErrForbidden
	}
	return nil
}

// A sealed plan is an expiring proposal, not an authorization. It is never
// returned through ordinary user APIs and contains no OAuth bearer tokens.
type sourceRebindPlan struct {
	ID, Operator         string
	CreatedAt, ExpiresAt time.Time
	Items                []sourceRebindItem
}

type sourceRebindItem struct {
	Source                                                   types.DataSource
	Import                                                   types.FeishuPersonalImport
	Credential                                               types.FeishuCredentialRef
	Audience, ScopeHash, PolicyHash, ExecutionHash, Revision string
	IndexHash                                                string
	// Credential's non-JSON fields are deliberately serialized only inside the
	// encrypted operator proposal. Public API refs omit them by design.
	OpenID, GrantEpoch, ConfigVersion, AppID string
	SourceState                              string
	SourceDeferrals                          types.JSON
}

type sourceRebindGrant struct {
	ID, PlanHash, AuthorizationRef, Operator string
	Plan                                     string
	Approvals                                map[string]string
	ExpiresAt                                time.Time
}

type sourceRebindReceipt struct {
	ID, Plan, MigrationApproval, BackupSHA, Operator, PublicationGrant string
	After                                                              map[string]string
	RolledBack                                                         bool
}

// PrepareSourceRebind is read-only apart from the existing legitimate token
// refresh path. It never migrates sources, grants publication or releases hold.
func (s *FeishuPersonalService) PrepareSourceRebind(
	ctx context.Context,
	ids []string,
	operator string,
) (string, error) {
	if !types.IsSystemAdminFromContext(ctx) || operator == "" || len(ids) == 0 || len(ids) > 16 {
		return "", access.ErrForbidden
	}
	if err := requireRebindOperator(ctx, s.db.WithContext(ctx), operator); err != nil {
		return "", err
	}
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	plan := sourceRebindPlan{
		ID:        uuid.NewString(),
		Operator:  operator,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}
	for i, id := range ids {
		if id == "" || i > 0 && ids[i-1] == id {
			return "", access.ErrForbidden
		}
		item, err := s.inspectSourceRebind(ctx, id)
		if err != nil {
			return "", err
		}
		plan.Items = append(plan.Items, *item)
		if i > 0 && (item.Import.UserID != plan.Items[0].Import.UserID ||
			item.Source.KnowledgeBaseID != plan.Items[0].Source.KnowledgeBaseID) {
			return "", access.ErrForbidden
		}
	}
	return sealFeishu(plan)
}

func sourceExecutionHash(tx *gorm.DB, id string) (string, error) {
	var logs []types.SyncLog
	if err := tx.Where("data_source_id = ?", id).Order("id").Find(&logs).Error; err != nil {
		return "", err
	}
	for _, l := range logs {
		if l.Status == types.SyncLogStatusRunning {
			return "", access.ErrForbidden
		}
	}
	b, err := json.Marshal(logs)
	return syncPolicyHash(b), err
}

// PostgreSQL computes a content-free digest in place; no document, chunk or
// embedding body leaves the DB. A changed published/index row blocks migration
// or rollback. It does not rebuild or rewrite any retrieval structure.
func sourceRebindIndexHash(tx *gorm.DB, knowledgeID string) (string, error) {
	if tx.Name() != "postgres" {
		return "", access.ErrForbidden
	}
	parts := []string{}
	for _, table := range []string{"knowledges", "chunks", "embeddings"} {
		column := "knowledge_id"
		if table == "knowledges" {
			column = "id"
		}
		var digest string
		query := "SELECT md5(COALESCE(string_agg(md5(row_to_json(t)::text),'' ORDER BY id),'')) " +
			"FROM (SELECT * FROM " + table + " WHERE " + column + " = ?) t"
		if err := tx.Raw(query, knowledgeID).Scan(&digest).Error; err != nil {
			return "", err
		}
		parts = append(parts, digest)
	}
	return rebindDigest(parts), nil
}

func rebindSettingHash(tx *gorm.DB, key string) (string, error) {
	var row types.SystemSetting
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("key = ?", key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}
	return syncPolicyHash(row.Value), nil
}

func (s *FeishuPersonalService) inspectSourceRebind(
	ctx context.Context,
	id string,
) (*sourceRebindItem, error) {
	var ds types.DataSource
	if err := s.db.WithContext(ctx).First(&ds, "id = ?", id).Error; err != nil {
		return nil, err
	}
	if !legacyWikiDrive(ds.Type) {
		return nil, access.ErrForbidden
	}
	config, err := ds.ParseConfig()
	if err != nil {
		return nil, err
	}
	ref, err := types.ParseFeishuSourceReference(config)
	if err != nil || ref == nil || ref.Kind != types.FeishuUserOAuth {
		return nil, access.ErrForbidden
	}
	var imported types.FeishuPersonalImport
	if err = s.db.WithContext(ctx).Where("id = ? AND user_id = ? AND kb_id = ? AND app_id = ? AND auth_mode = ?",
		id, ref.OwnerUserID, ds.KnowledgeBaseID, ref.AppID, types.FeishuUserOAuth).First(&imported).Error; err != nil {
		return nil, err
	}
	// Original import is the independent same-account proof. Matching App alone
	// cannot establish identity after an account switch.
	if imported.OpenID == "" || imported.KnowledgeID == "" || imported.SyncLease != "" {
		return nil, access.ErrForbidden
	}
	metadataCtx := types.WithLegacyKBAdminMetadata(ctx, ds.KnowledgeBaseID)
	fresh, err := s.sourceRebindOwnerContext(
		metadataCtx,
		&types.FeishuBatch{
			OwnerID:  ref.OwnerUserID,
			TenantID: ds.TenantID,
			KBID:     ds.KnowledgeBaseID,
		},
	)
	if err != nil {
		return nil, err
	}
	remote, client, current, err := s.ResolveCredentialReference(fresh, types.FeishuUserOAuth, "")
	if err != nil {
		return nil, err
	}
	if current.AppID != ref.AppID || current.OpenID != imported.OpenID ||
		current.ConfigVersion != ref.IdentityVersion ||
		current.ConnectionID == ref.ConnectionID {
		return nil, access.ErrForbidden
	}
	links, reference, err := core.ReferenceLinks(config)
	if err != nil || !reference || len(links) != 1 {
		return nil, access.ErrForbidden
	}
	r, err := core.ParsePersonalResourceURL(links[0])
	if err != nil {
		return nil, err
	}
	remote, cancel := context.WithTimeout(remote, 30*time.Second)
	defer cancel()
	r, err = client.ResolveBoundResource(applicationTableContext(remote), r)
	if err != nil {
		return nil, err
	}
	if r.Type != imported.ResourceType || r.Token != imported.DocumentToken || r.Modified == "" {
		return nil, access.ErrForbidden
	}
	permissions, err := client.InspectPersonalPermissions(remote, r)
	if err != nil {
		return nil, err
	}
	if !permissions.View || !permissions.Copy || !permissions.Export {
		return nil, core.ErrPersonalBlocksPolicyUnverified
	}
	var connection types.FeishuConnection
	if err = s.db.WithContext(ctx).
		Where("user_id = ? AND app_id = ? AND open_id = ?", ref.OwnerUserID, ref.AppID, current.OpenID).
		First(&connection).
		Error; err != nil {
		return nil, err
	}
	var tokens feishuTokens
	if err = openFeishu(connection.Tokens, &tokens); err != nil {
		return nil, err
	}
	// Scope inventory is evidence, not a substitute for the actual three
	// permission calls above. No unknown/empty consent inventory is accepted.
	if !rebindScopesValid(tokens.Scope, imported.ResourceType) {
		return nil, core.ErrUserScope
	}
	item := &sourceRebindItem{
		Source: ds, Import: imported, Credential: current, OpenID: current.OpenID,
		GrantEpoch: current.GrantEpoch, ConfigVersion: current.ConfigVersion, AppID: current.AppID,
		ScopeHash: syncPolicyHash([]byte(tokens.Scope)), Revision: r.Modified,
		SourceState: ds.LegacyAccessState, SourceDeferrals: ds.DocumentDeferrals,
	}
	err = s.db.WithContext(metadataCtx).Transaction(func(tx *gorm.DB) error {
		var e error
		item.Audience, e = syncPolicyAudience(tx, ds.KnowledgeBaseID)
		if e != nil {
			return e
		}
		item.PolicyHash, e = rebindSettingHash(tx, feishuSyncPolicyPrefix+id)
		if e != nil {
			return e
		}
		item.ExecutionHash, e = sourceExecutionHash(tx, id)
		if e != nil {
			return e
		}
		item.IndexHash, e = sourceRebindIndexHash(tx, imported.KnowledgeID)
		return e
	})
	return item, err
}

// Rebinding checks live owner authority directly, independently of the ingestion
// dispatcher. A stopped-worker operator must not need a running parser pipeline
// merely to verify source identity. This does not grant access to KB contents.
func (s *FeishuPersonalService) sourceRebindOwnerContext(
	ctx context.Context, batch *types.FeishuBatch,
) (context.Context, error) {
	return s.sourceOwnerContext(ctx, batch, func(tx *gorm.DB) error {
		return lockBatchPublicationAuthority(tx, batch)
	})
}

func (s *FeishuPersonalService) sourceConsentOwnerContext(
	ctx context.Context, batch *types.FeishuBatch,
) (context.Context, error) {
	return s.sourceOwnerContext(ctx, batch, func(tx *gorm.DB) error {
		return s.requireImportWriter(tx, batch.OwnerID, batch.KBID, batch.TenantID)
	})
}

func (s *FeishuPersonalService) sourceOwnerContext(
	ctx context.Context, batch *types.FeishuBatch, authority func(*gorm.DB) error,
) (context.Context, error) {
	var user types.User
	var member types.TenantMember
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authority(tx); err != nil {
			return err
		}
		owner, err := types.PrivateKBOwner(tx, batch.KBID)
		if err != nil || owner != "" && owner != batch.OwnerID {
			return access.ErrForbidden
		}
		if err := tx.First(&user, "id = ?", batch.OwnerID).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ? AND tenant_id = ?", batch.OwnerID, batch.TenantID).
			First(&member).
			Error
	})
	if err != nil {
		return ctx, err
	}
	fresh := context.WithValue(ctx, types.KBGrantsContextKey, nil)
	fresh = types.WithCaller(
		fresh,
		types.Caller{TenantID: batch.TenantID, UserID: batch.OwnerID, Role: member.Role},
	)
	fresh = context.WithValue(fresh, types.UserIDContextKey, batch.OwnerID)
	fresh = types.WithPrincipal(
		fresh,
		types.Principal{Type: types.PrincipalWebUser, ID: batch.OwnerID},
	)
	return context.WithValue(fresh, types.SystemAdminContextKey, user.IsSystemAdmin), nil
}

func rebindScopesValid(scope, kind string) bool {
	set := map[string]bool{}
	for _, s := range strings.Fields(scope) {
		set[s] = true
	}
	for _, s := range []string{"docs:permission.member:auth", "drive:drive.metadata:readonly", "docs:document:export"} {
		if !set[s] {
			return false
		}
	}
	switch kind {
	case "docx":
		return set["docx:document:readonly"]
	case "sheet":
		return set["sheets:spreadsheet:readonly"]
	case "bitable":
		return set["bitable:app:readonly"]
	case "file":
		return set["drive:file:readonly"]
	default:
		return false
	}
}

func readRebindPlan(cipher string) (*sourceRebindPlan, error) {
	var plan sourceRebindPlan
	if openFeishu(cipher, &plan) != nil || plan.ID == "" || len(plan.Items) == 0 ||
		len(plan.Items) > 16 ||
		!plan.ExpiresAt.After(time.Now()) {
		return nil, access.ErrForbidden
	}
	restoreRebindIdentity(&plan)
	return &plan, nil
}

// Public source/import DTOs deliberately omit identity fields. Restore them
// only from explicit fields within this AES-GCM authenticated operator plan.
func restoreRebindIdentity(plan *sourceRebindPlan) {
	for i := range plan.Items {
		item := &plan.Items[i]
		item.Import.AppID, item.Import.OpenID = item.AppID, item.OpenID
		item.Credential.OpenID, item.Credential.GrantEpoch = item.OpenID, item.GrantEpoch
		item.Credential.ConfigVersion = item.ConfigVersion
		item.Source.LegacyAccessState, item.Source.DocumentDeferrals = item.SourceState, item.SourceDeferrals
	}
}

func rebindDigest(value any) string { b, _ := json.Marshal(value); return syncPolicyHash(b) }

func createRebindSetting(tx *gorm.DB, key, operator string, value any) error {
	cipher, err := sealFeishu(value)
	if err != nil {
		return err
	}
	b, err := json.Marshal(cipher)
	if err != nil {
		return err
	}
	return tx.Create(&types.SystemSetting{
		Key: key, Value: b, ValueType: "string", Category: "integration",
		IsSecret: true, LastModifiedBy: operator,
	}).Error
}

// checkRebindSnapshot runs under the source UPDATE lock. It also locks identity,
// audience and authority; a source/epoch/policy/execution change invalidates the
// whole proposal. Remote checks happen before, never while holding DB locks.
func checkRebindSnapshot(tx *gorm.DB, item sourceRebindItem) error {
	var ds types.DataSource
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ds, "id = ?", item.Source.ID).Error; err != nil {
		return err
	}
	if rebindDigest(ds) != rebindDigest(item.Source) || ds.LegacyAccessState != item.SourceState ||
		rebindDigest(ds.DocumentDeferrals) != rebindDigest(item.SourceDeferrals) {
		return ErrFeishuRebindConflict
	}
	var imported types.FeishuPersonalImport
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&imported, "id = ?", ds.ID).Error; err != nil {
		return err
	}
	if rebindDigest(imported) != rebindDigest(item.Import) || imported.OpenID != item.OpenID ||
		imported.SyncLease != "" {
		return ErrFeishuRebindConflict
	}
	batch := &types.FeishuBatch{
		OwnerID:  item.Import.UserID,
		TenantID: ds.TenantID,
		KBID:     ds.KnowledgeBaseID,
	}
	if err := lockBatchPublicationAuthority(tx, batch); err != nil {
		return err
	}
	var connection types.FeishuConnection
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("user_id = ? AND app_id = ? AND open_id = ?",
		item.Import.UserID, item.Import.AppID, item.OpenID).First(&connection).Error; err != nil {
		return ErrFeishuReconnect
	}
	var tokens feishuTokens
	if openFeishu(connection.Tokens, &tokens) != nil ||
		syncPolicyHash([]byte(tokens.Scope)) != item.ScopeHash {
		return ErrFeishuReconnect
	}
	ref := &types.FeishuSourceReference{
		Kind:         types.FeishuUserOAuth,
		OwnerUserID:  item.Import.UserID,
		AppID:        item.Import.AppID,
		ConnectionID: item.Credential.ConnectionID,
	}
	if err := validatePersonalSourceBinding(tx, ref); err != nil {
		return err
	}
	var epoch types.FeishuPersonalGrantEpoch
	if err := tx.Where("user_id = ? AND epoch = ?", item.Import.UserID, item.GrantEpoch).
		First(&epoch).
		Error; err != nil {
		return ErrFeishuReconnect
	}
	var setting types.SystemSetting
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("key = ?", feishuAppSetting).
		First(&setting).
		Error; err != nil {
		return err
	}
	cipher, err := setting.AsString()
	if err != nil {
		return err
	}
	var app FeishuAppConfig
	if openFeishu(cipher, &app) != nil || app.AppID != ref.AppID || !app.modeEnabled(ref.Kind) ||
		app.identityVersion(ref.Kind) != item.ConfigVersion {
		return ErrFeishuReconnect
	}
	audience, err := syncPolicyAudience(tx, ds.KnowledgeBaseID)
	if err != nil {
		return err
	}
	policy, err := rebindSettingHash(tx, feishuSyncPolicyPrefix+ds.ID)
	if err != nil {
		return err
	}
	execution, err := sourceExecutionHash(tx, ds.ID)
	if err != nil {
		return err
	}
	index, err := sourceRebindIndexHash(tx, imported.KnowledgeID)
	if err != nil {
		return err
	}
	if audience != item.Audience || policy != item.PolicyHash || execution != item.ExecutionHash ||
		index != item.IndexHash {
		return ErrFeishuRebindConflict
	}
	return nil
}

// AuthorizeSourceRebindPublication is a SEPARATE explicit administrative act.
// It records a fresh, current-revision approval for each selected resource and
// binds it to the current connection/epoch in the encrypted grant. Never called
// by OAuth callbacks, ordinary import, Prepare or Apply implicitly.
func (s *FeishuPersonalService) AuthorizeSourceRebindPublication(
	ctx context.Context,
	proposal, operator, authorization string,
	expires time.Time,
) (string, error) {
	if os.Getenv("FEISHU_SOURCE_REBIND_OPERATIONS_ENABLED") != "1" ||
		!types.IsSystemAdminFromContext(ctx) ||
		operator == "" ||
		strings.TrimSpace(authorization) == "" ||
		!expires.After(time.Now()) ||
		expires.After(time.Now().Add(24*time.Hour)) {
		return "", access.ErrForbidden
	}
	plan, err := readRebindPlan(proposal)
	if err != nil {
		return "", err
	}
	grant := sourceRebindGrant{
		Plan:             proposal,
		ID:               uuid.NewString(),
		PlanHash:         syncPolicyHash([]byte(proposal)),
		Operator:         operator,
		AuthorizationRef: authorization,
		ExpiresAt:        expires,
		Approvals:        map[string]string{},
	}
	err = s.db.WithContext(types.WithLegacyKBAdminMetadata(ctx, plan.Items[0].Source.KnowledgeBaseID)).
		Transaction(func(tx *gorm.DB) error {
			if err := requireRebindOperator(ctx, tx, operator); err != nil {
				return err
			}
			for _, item := range plan.Items {
				if err := checkRebindSnapshot(tx, item); err != nil {
					return err
				}
				approval := types.FeishuSharingApproval{
					ID: uuid.NewString(), OwnerID: item.Import.UserID, KBID: item.Import.KBID,
					AppID: item.Import.AppID, ResourceType: item.Import.ResourceType,
					ResourceToken: item.Import.DocumentToken,
					Revision:      item.Revision, ConfigVersion: item.ConfigVersion,
					State: "approved", ApprovedBy: operator,
					Reason: authorization, CreatedAt: time.Now().UTC(), ExpiresAt: expires,
				}
				if err := tx.Create(&approval).Error; err != nil {
					return err
				}
				grant.Approvals[item.Source.ID] = approval.ID
			}
			return createRebindSetting(tx, sourceRebindPrefix+"grant."+grant.ID, operator, grant)
		})
	return grant.ID, err
}

// ApplySourceRebind changes only selected credential bindings atomically. A
// tested backup receipt, fresh publication grant and distinct migration approval
// are mandatory. It cannot enqueue, clear trip, change hold or advance Cursor.
func (s *FeishuPersonalService) ApplySourceRebind(
	ctx context.Context,
	proposal, grantID, operator, migrationApproval, backupSHA string,
) (string, error) {
	if os.Getenv("FEISHU_SOURCE_REBIND_OPERATIONS_ENABLED") != "1" ||
		!types.IsSystemAdminFromContext(ctx) ||
		operator == "" ||
		migrationApproval == "" ||
		len(backupSHA) != 64 {
		return "", access.ErrForbidden
	}
	if err := requireRebindOperator(ctx, s.db.WithContext(ctx), operator); err != nil {
		return "", err
	}
	plan, err := readRebindPlan(proposal)
	if err != nil {
		return "", err
	}
	for _, item := range plan.Items {
		fresh, e := s.inspectSourceRebind(ctx, item.Source.ID)
		if e != nil {
			return "", e
		}
		if rebindDigest(fresh) != rebindDigest(item) {
			return "", ErrFeishuRebindConflict
		}
	}
	receipt := sourceRebindReceipt{
		ID: plan.ID, Plan: proposal, MigrationApproval: migrationApproval, BackupSHA: backupSHA,
		Operator: operator, PublicationGrant: grantID, After: map[string]string{},
	}
	err = s.db.WithContext(types.WithLegacyKBAdminMetadata(ctx, plan.Items[0].Source.KnowledgeBaseID)).
		Transaction(func(tx *gorm.DB) error {
			if err := requireRebindOperator(ctx, tx, operator); err != nil {
				return err
			}
			var row types.SystemSetting
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("key = ?", sourceRebindPrefix+"grant."+grantID).
				First(&row).
				Error; err != nil {
				return err
			}
			cipher, e := row.AsString()
			if e != nil {
				return e
			}
			var grant sourceRebindGrant
			if openFeishu(cipher, &grant) != nil ||
				grant.PlanHash != syncPolicyHash([]byte(proposal)) ||
				!grant.ExpiresAt.After(time.Now()) ||
				grant.AuthorizationRef == migrationApproval {
				return access.ErrForbidden
			}
			for _, item := range plan.Items {
				if err := checkRebindSnapshot(tx, item); err != nil {
					return err
				}
				var approval types.FeishuSharingApproval
				if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND state = ? AND expires_at > ?",
					grant.Approvals[item.Source.ID], "approved", time.Now()).First(&approval).Error; err != nil {
					return ErrFeishuSharingApproval
				}
				// Patch the original JSON object. Do not serialize/decrypt/re-encrypt
				// the rest of the connector config or touch its cursor or identity.
				var config map[string]json.RawMessage
				if json.Unmarshal(item.Source.Config, &config) != nil {
					return access.ErrForbidden
				}
				var settings map[string]json.RawMessage
				if json.Unmarshal(config["settings"], &settings) != nil {
					return access.ErrForbidden
				}
				var ref types.FeishuSourceReference
				if json.Unmarshal(settings["credential_reference"], &ref) != nil {
					return access.ErrForbidden
				}
				ref.ConnectionID = item.Credential.ConnectionID
				settings["credential_reference"], _ = json.Marshal(ref)
				config["settings"], _ = json.Marshal(settings)
				next, e := json.Marshal(config)
				if e != nil {
					return e
				}
				updated := tx.Model(&types.DataSource{}).
					Where("id = ? AND config = ?", item.Source.ID, string(item.Source.Config)).
					UpdateColumn("config", types.JSON(next))
				if updated.Error != nil {
					return updated.Error
				}
				if updated.RowsAffected != 1 {
					return ErrFeishuRebindConflict
				}
				if item.PolicyHash != "absent" {
					// Archive immutable evidence; never carry the old authorization
					// into a fresh epoch or restore it during rollback.
					var old types.SystemSetting
					if e = tx.Where("key = ?", feishuSyncPolicyPrefix+item.Source.ID).First(&old).Error; e != nil {
						return e
					}
					old.ID = 0
					old.Key = sourceRebindPrefix + "old." + plan.ID + "." + item.Source.ID
					if e = tx.Create(&old).Error; e != nil {
						return e
					}
					if e = tx.Where("key = ?", feishuSyncPolicyPrefix+item.Source.ID).
						Delete(&types.SystemSetting{}).
						Error; e != nil {
						return e
					}
				}
				var after types.DataSource
				if e = tx.First(&after, "id = ?", item.Source.ID).Error; e != nil {
					return e
				}
				receipt.After[item.Source.ID] = rebindDigest(after)
				// The active binding points at its NEW authorization, so later
				// revocation/expiry cannot fall back to a still-approved old row.
				marker := struct{ GrantID, ConnectionID string }{
					grantID,
					item.Credential.ConnectionID,
				}
				if e = writeRebindMarker(tx, item.Source.ID, operator, marker); e != nil {
					return e
				}
			}
			return createRebindSetting(tx, sourceRebindPrefix+"receipt."+plan.ID, operator, receipt)
		})
	return receipt.ID, err
}

func writeRebindMarker(tx *gorm.DB, id, operator string, value any) error {
	cipher, err := sealFeishu(value)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(cipher)
	if err != nil {
		return err
	}
	update := clause.OnConflict{
		Columns: []clause.Column{{Name: "key"}},
		DoUpdates: clause.Assignments(
			map[string]any{"value": types.JSON(raw), "last_modified_by": operator},
		),
	}
	return tx.Clauses(update).
		Create(&types.SystemSetting{
			Key: sourceRebindPrefix + "current." + id, Value: raw, ValueType: "string", Category: "integration",
			IsSecret: true, LastModifiedBy: operator,
		}).
		Error
}

func checkRebindPublication(tx *gorm.DB, sourceID, connectionID string) error {
	return checkSourceRebindAuthority(tx, sourceID, connectionID, "", time.Now())
}

// priorEpoch is only used by a server-recorded, same-account consent transition.
// It checks the source's previous authority while the caller separately locks
// and verifies the new live grant. Ordinary reads always use the live epoch.
func checkSourceRebindAuthority(tx *gorm.DB, sourceID, connectionID, priorEpoch string, now time.Time) error {
	var row types.SystemSetting
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("key = ?", sourceRebindPrefix+"current."+sourceID).
		First(&row).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	cipher, err := row.AsString()
	if err != nil {
		return err
	}
	var marker sourceBindingMarker
	if openFeishu(cipher, &marker) != nil || marker.ConnectionID != connectionID {
		return ErrFeishuSharingApproval
	}
	if marker.ConsentReceipt != "" {
		return checkConsentBindingReceipt(tx, sourceID, marker, priorEpoch)
	}
	var grantRow types.SystemSetting
	if err = tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("key = ?", sourceRebindPrefix+"grant."+marker.GrantID).
		First(&grantRow).
		Error; err != nil {
		return err
	}
	cipher, err = grantRow.AsString()
	if err != nil {
		return err
	}
	var grant sourceRebindGrant
	if openFeishu(cipher, &grant) != nil || grant.ID != marker.GrantID {
		return ErrFeishuSharingApproval
	}
	var plan sourceRebindPlan
	if openFeishu(grant.Plan, &plan) != nil {
		return ErrFeishuSharingApproval
	}
	restoreRebindIdentity(&plan)
	// Expiration limits the administrative migration/recovery operation. Once
	// that operation and a verified sync completed, normal updates use current
	// OAuth/source permissions and KB write ACLs, rather than renewing a daily
	// sharing approval. Revoked/rolled-back authority and held sources still deny.
	var applied types.SystemSetting
	if err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("key = ?",
		sourceRebindPrefix+"receipt."+plan.ID).First(&applied).Error; err != nil {
		return ErrFeishuSharingApproval
	}
	appliedCipher, err := applied.AsString()
	if err != nil {
		return err
	}
	var receipt sourceRebindReceipt
	if openFeishu(appliedCipher, &receipt) != nil || receipt.RolledBack || receipt.ID != plan.ID ||
		receipt.Plan != grant.Plan || receipt.PublicationGrant != grant.ID ||
		receipt.Operator != grant.Operator || receipt.MigrationApproval == "" ||
		receipt.MigrationApproval == grant.AuthorizationRef || len(receipt.BackupSHA) != 64 ||
		applied.CreatedAt.IsZero() || applied.CreatedAt.After(grant.ExpiresAt) {
		return ErrFeishuSharingApproval
	}
	matched := false
	for _, item := range plan.Items {
		if item.Source.ID != sourceID {
			continue
		}
		if item.Credential.ConnectionID != connectionID {
			return ErrFeishuSharingApproval
		}
		audience, e := syncPolicyAudience(tx, item.Source.KnowledgeBaseID)
		if e != nil {
			return e
		}
		if audience != item.Audience {
			return ErrFeishuSharingApproval
		}
		if priorEpoch != "" {
			if priorEpoch != item.GrantEpoch {
				return ErrFeishuSharingApproval
			}
		} else {
			var epoch types.FeishuPersonalGrantEpoch
			if e = tx.Clauses(clause.Locking{Strength: "SHARE"}).
				Where("user_id = ? AND epoch = ?", item.Import.UserID, item.GrantEpoch).
				First(&epoch).Error; e != nil {
				return ErrFeishuSharingApproval
			}
		}
		matched = true
	}
	if !matched {
		return ErrFeishuSharingApproval
	}
	var approval types.FeishuSharingApproval
	if err = tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND state = ?", grant.Approvals[sourceID], "approved").
		First(&approval).
		Error; err != nil {
		return ErrFeishuSharingApproval
	}
	if !grant.ExpiresAt.After(now) || !approval.ExpiresAt.After(now) {
		for _, item := range plan.Items {
			if item.Source.ID == sourceID {
				return requireCompletedSourceRebind(tx, item)
			}
		}
		return ErrFeishuSharingApproval
	}
	return nil
}

// RollbackSourceRebind is a new explicitly authorized administrative act, not an
// error-handler side effect. It restores the whole batch only if no source,
// cursor, import or execution has changed since Apply. Old publication policies
// stay archived; fresh approvals are revoked. Holds and the global stop remain.
func (s *FeishuPersonalService) RollbackSourceRebind(
	ctx context.Context,
	receiptID, operator, approvalRef string,
) error {
	if os.Getenv("FEISHU_SOURCE_REBIND_OPERATIONS_ENABLED") != "1" ||
		!types.IsSystemAdminFromContext(ctx) ||
		operator == "" ||
		approvalRef == "" {
		return access.ErrForbidden
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireRebindOperator(ctx, tx, operator); err != nil {
			return err
		}
		var row types.SystemSetting
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("key = ?", sourceRebindPrefix+"receipt."+receiptID).
			First(&row).
			Error; err != nil {
			return err
		}
		cipher, err := row.AsString()
		if err != nil {
			return err
		}
		var receipt sourceRebindReceipt
		if openFeishu(cipher, &receipt) != nil || receipt.RolledBack {
			return access.ErrForbidden
		}
		var plan sourceRebindPlan
		if openFeishu(receipt.Plan, &plan) != nil {
			return access.ErrForbidden
		}
		restoreRebindIdentity(&plan)
		for _, item := range plan.Items {
			var current types.DataSource
			if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				First(&current, "id = ?", item.Source.ID).
				Error; err != nil {
				return err
			}
			if rebindDigest(current) != receipt.After[item.Source.ID] ||
				current.LegacyAccessState != item.SourceState ||
				rebindDigest(current.DocumentDeferrals) != rebindDigest(item.SourceDeferrals) {
				return ErrFeishuRebindConflict
			}
			execution, e := sourceExecutionHash(tx, current.ID)
			if e != nil {
				return e
			}
			if execution != item.ExecutionHash {
				return ErrFeishuRebindConflict
			}
			index, e := sourceRebindIndexHash(tx, item.Import.KnowledgeID)
			if e != nil {
				return e
			}
			if index != item.IndexHash {
				return ErrFeishuRebindConflict
			}
			var imported types.FeishuPersonalImport
			if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				First(&imported, "id = ?", item.Source.ID).
				Error; e != nil {
				return e
			}
			if rebindDigest(imported) != rebindDigest(item.Import) ||
				imported.OpenID != item.OpenID ||
				imported.SyncLease != "" {
				return ErrFeishuRebindConflict
			}
			policy, e := rebindSettingHash(tx, feishuSyncPolicyPrefix+current.ID)
			if e != nil {
				return e
			}
			if policy != "absent" {
				return ErrFeishuRebindConflict
			}
			restored := tx.Model(&types.DataSource{}).
				Where("id = ? AND config = ?", current.ID, string(current.Config)).
				UpdateColumn("config", item.Source.Config)
			if restored.Error != nil {
				return restored.Error
			}
			if restored.RowsAffected != 1 {
				return ErrFeishuRebindConflict
			}
		}
		var grantRow types.SystemSetting
		if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("key = ?", sourceRebindPrefix+"grant."+receipt.PublicationGrant).
			First(&grantRow).
			Error; err != nil {
			return err
		}
		grantCipher, err := grantRow.AsString()
		if err != nil {
			return err
		}
		var grant sourceRebindGrant
		if openFeishu(grantCipher, &grant) != nil {
			return access.ErrForbidden
		}
		for _, id := range grant.Approvals {
			if err = tx.Model(&types.FeishuSharingApproval{}).
				Where("id = ?", id).
				Update("state", "revoked").
				Error; err != nil {
				return err
			}
		}
		return createRebindSetting(tx, sourceRebindPrefix+"rollback."+receiptID, operator, struct {
			ReceiptID, AuthorizationRef string
			At                          time.Time
		}{receiptID, approvalRef, time.Now().UTC()})
	})
}
