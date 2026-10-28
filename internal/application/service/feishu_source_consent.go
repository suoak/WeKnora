package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const sourceConsentPrefix = "integration.feishu.consent."

// Only a positively identified same-account consent transition is transient.
// Unknown identity, scope loss and source permission denials remain fail closed.
var errSourceConsentPending = errors.Join(core.ErrTransient, errors.New("feishu_source_authorization_updating"))

type sourceBindingMarker struct {
	GrantID, ConnectionID, ConsentReceipt string
}

type sourceConsentItem struct {
	SourceID, KBID, KnowledgeID, OldConnectionID, OldEpoch, ConfigHash string
	TenantID                                                           uint64
	OriginalConfig                                                     types.JSON
}

type sourceConsentTransition struct {
	Epoch, UserID, AppID, OpenID, ConfigVersion, ConnectionID string
	CreatedAt, ExpiresAt                                      time.Time
	Items                                                     map[string]sourceConsentItem
}

type sourceConsentReceipt struct {
	ID, Epoch, UserID, AppID, OpenID, ConfigVersion, ConnectionID string
	SourceID, KBID, KnowledgeID, ConfigHash, Audience             string
	TenantID                                                      uint64
	CreatedAt                                                     time.Time
}

func readSourceSetting(tx *gorm.DB, key string, value any) error {
	var row types.SystemSetting
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("key = ?", key).First(&row).Error; err != nil {
		return err
	}
	cipher, err := row.AsString()
	if err != nil {
		return err
	}
	return openFeishu(cipher, value)
}

func sourceMayRenewConsent(ds *types.DataSource) bool {
	var deferrals []json.RawMessage
	return ds != nil && ds.LegacyAccessState == "active" && (ds.Status == types.DataSourceStatusActive ||
		ds.Status == types.DataSourceStatusError) &&
		json.Unmarshal(ds.DocumentDeferrals, &deferrals) == nil && len(deferrals) == 0
}

// Called inside the OAuth commit, before replacing the old connection/epoch.
// No source is changed, no document API is called, and no task/hold is resumed.
// The immutable encrypted snapshot makes the later worker action auditable.
func (s *FeishuPersonalService) recordSourceConsent(tx *gorm.DB, next *types.FeishuConnection,
	cfg *FeishuAppConfig, epoch string,
) error {
	if tx.Name() != "postgres" || !tx.Migrator().HasTable(&types.DataSource{}) ||
		!tx.Migrator().HasTable(&types.FeishuPersonalGrantEpoch{}) ||
		!tx.Migrator().HasTable(&types.FeishuPersonalImport{}) {
		return nil
	}
	var old types.FeishuConnection
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", next.UserID).First(&old).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil && (old.AppID != next.AppID || old.OpenID != next.OpenID) {
		return nil
	}
	var previous types.FeishuPersonalGrantEpoch
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?",
		next.UserID).First(&previous).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var prior sourceConsentTransition
	err = readSourceSetting(tx, sourceConsentPrefix+previous.Epoch, &prior)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	oldID := connectionID(types.FeishuUserOAuth, next.UserID, next.AppID, next.OpenID, previous.Epoch)
	transition := sourceConsentTransition{
		Epoch: epoch, UserID: next.UserID, AppID: next.AppID, OpenID: next.OpenID,
		ConfigVersion: cfg.identityVersion(types.FeishuUserOAuth),
		ConnectionID:  connectionID(types.FeishuUserOAuth, next.UserID, next.AppID, next.OpenID, epoch),
		CreatedAt:     time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		Items: map[string]sourceConsentItem{},
	}
	var sources []types.DataSource
	// MVCC reads deliberately take no source locks while the connection is locked.
	// The worker rechecks the exact snapshot under its execution/source locks.
	if err := tx.Where("id IN (?) AND legacy_access_state = ? AND status IN ?",
		tx.Model(&types.FeishuPersonalImport{}).Select("id").Where(
			"user_id = ? AND app_id = ? AND open_id = ? AND auth_mode = ? AND status = ?",
			next.UserID, next.AppID, next.OpenID, types.FeishuUserOAuth, "source_managed"), "active",
		[]string{types.DataSourceStatusActive, types.DataSourceStatusError}).Find(&sources).Error; err != nil {
		return err
	}
	for _, ds := range sources {
		if !sourceMayRenewConsent(&ds) {
			continue
		}
		config, err := ds.ParseConfig()
		if err != nil {
			continue
		}
		ref, err := types.ParseFeishuSourceReference(config)
		if err != nil || ref == nil || ref.Kind != types.FeishuUserOAuth || ref.OwnerUserID != next.UserID ||
			ref.AppID != next.AppID || ref.IdentityVersion != transition.ConfigVersion {
			continue
		}
		var imported types.FeishuPersonalImport
		if err := tx.Where("id = ? AND kb_id = ? AND user_id = ? AND app_id = ? AND open_id = ?", ds.ID,
			ds.KnowledgeBaseID, next.UserID, next.AppID, next.OpenID).First(&imported).Error; err != nil {
			return err
		}
		item := sourceConsentItem{
			SourceID: ds.ID, KBID: ds.KnowledgeBaseID, TenantID: ds.TenantID,
			KnowledgeID:     imported.KnowledgeID,
			OldConnectionID: ref.ConnectionID, OldEpoch: previous.Epoch, ConfigHash: syncPolicyHash(ds.Config),
			OriginalConfig: ds.Config,
		}
		if ref.ConnectionID != oldID {
			pending, exists := prior.Items[ds.ID]
			if !exists || prior.Epoch != previous.Epoch || prior.UserID != next.UserID ||
				prior.AppID != next.AppID || prior.OpenID != next.OpenID ||
				prior.ConfigVersion != transition.ConfigVersion || prior.ConnectionID != oldID ||
				pending.ConfigHash != item.ConfigHash ||
				pending.OldConnectionID != ref.ConnectionID || pending.KBID != ds.KnowledgeBaseID ||
				pending.TenantID != ds.TenantID || pending.KnowledgeID != imported.KnowledgeID {
				continue
			}
			item.OldEpoch = pending.OldEpoch
		}
		if item.KnowledgeID != "" {
			transition.Items[ds.ID] = item
		}
	}
	if len(transition.Items) == 0 {
		return nil
	}
	return createRebindSetting(tx, sourceConsentPrefix+epoch, next.UserID, transition)
}

func loadSourceConsent(tx *gorm.DB, ds *types.DataSource,
	ref *types.FeishuSourceReference,
) (*sourceConsentTransition, *sourceConsentItem, error) {
	if !sourceMayRenewConsent(ds) || ref == nil || ref.Kind != types.FeishuUserOAuth {
		return nil, nil, ErrFeishuSourceRebindRequired
	}
	var epoch types.FeishuPersonalGrantEpoch
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("user_id = ?",
		ref.OwnerUserID).First(&epoch).Error; err != nil {
		return nil, nil, err
	}
	var record sourceConsentTransition
	if err := readSourceSetting(tx, sourceConsentPrefix+epoch.Epoch, &record); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrFeishuSourceRebindRequired
		}
		return nil, nil, err
	}
	item, ok := record.Items[ds.ID]
	if !ok || !record.ExpiresAt.After(time.Now()) || record.Epoch != epoch.Epoch || record.UserID != ref.OwnerUserID ||
		record.AppID != ref.AppID || record.ConfigVersion != ref.IdentityVersion || record.OpenID == "" ||
		record.ConnectionID != connectionID(types.FeishuUserOAuth, record.UserID, record.AppID, record.OpenID,
			record.Epoch) ||
		item.OldConnectionID != ref.ConnectionID || item.ConfigHash != syncPolicyHash(ds.Config) ||
		item.SourceID != ds.ID || item.TenantID != ds.TenantID || item.KBID != ds.KnowledgeBaseID ||
		item.OldConnectionID != connectionID(types.FeishuUserOAuth, record.UserID, record.AppID, record.OpenID,
			item.OldEpoch) {
		return nil, nil, ErrFeishuSourceRebindRequired
	}
	return &record, &item, nil
}

func sourceConsentFenceError(tx *gorm.DB, ds *types.DataSource, ref *types.FeishuSourceReference, cause error) error {
	if _, _, err := loadSourceConsent(tx, ds, ref); err == nil {
		return errSourceConsentPending
	}
	return cause
}

// Existing source Worker only: live permission checks precede a small fenced
// transaction. This never changes status, hold, Knowledge, ExternalID or Cursor.
func (s *FeishuPersonalService) renewSourceConsent(ctx context.Context, ds *types.DataSource) error {
	execution, ok := types.SyncExecutionFromContext(ctx)
	if !ok || execution.DataSourceID != ds.ID || execution.TenantID != ds.TenantID {
		return ErrFeishuSourceRebindRequired
	}
	config, err := ds.ParseConfig()
	if err != nil {
		return err
	}
	ref, err := types.ParseFeishuSourceReference(config)
	if err != nil {
		return err
	}
	record, item, err := loadSourceConsent(s.db.WithContext(ctx), ds, ref)
	if err != nil {
		return err
	}
	fresh, err := s.sourceRebindOwnerContext(types.WithLegacyKBAdminMetadata(ctx, ds.KnowledgeBaseID),
		&types.FeishuBatch{OwnerID: ref.OwnerUserID, TenantID: ds.TenantID, KBID: ds.KnowledgeBaseID})
	if err != nil {
		return err
	}
	remote, client, credential, err := s.ResolveCredentialReference(fresh, types.FeishuUserOAuth, record.ConnectionID)
	if err != nil {
		return err
	}
	if credential.OpenID != record.OpenID || credential.GrantEpoch != record.Epoch ||
		credential.ConfigVersion != record.ConfigVersion {
		return ErrFeishuReconnect
	}
	links, single, err := core.ReferenceLinks(config)
	if err != nil {
		return err
	}
	if !single || len(links) != 1 {
		return ErrFeishuSourceRebindRequired
	}
	r, err := core.ParsePersonalResourceURL(links[0])
	if err != nil {
		return err
	}
	remote, cancel := context.WithTimeout(applicationTableContext(remote), 30*time.Second)
	defer cancel()
	if err = feishusafety.Check(ctx); err != nil {
		return err
	}
	r, err = client.ResolveBoundResource(remote, r)
	if err != nil {
		return err
	}
	var imported types.FeishuPersonalImport
	if err = s.db.WithContext(ctx).Where("id = ? AND user_id = ? AND app_id = ? AND open_id = ? AND kb_id = ? AND "+
		"auth_mode = ?",
		ds.ID, record.UserID, record.AppID, record.OpenID, ds.KnowledgeBaseID,
		types.FeishuUserOAuth).First(&imported).Error; err != nil {
		return err
	}
	if imported.KnowledgeID != item.KnowledgeID || imported.ResourceType != r.Type ||
		imported.DocumentToken != r.Token || r.Modified == "" {
		return ErrFeishuSourceRebindRequired
	}
	permissions, err := client.InspectPersonalPermissions(remote, r)
	if err != nil {
		return err
	}
	switch {
	case !permissions.View:
		return core.ErrPersonalReadDenied
	case !permissions.Copy && !permissions.Export:
		return core.ErrPersonalCopyExportDenied
	case !permissions.Copy:
		return core.ErrPersonalCopyDenied
	case !permissions.Export:
		return core.ErrPersonalCopyOnlyReview
	}
	var after types.DataSource
	err = s.db.WithContext(types.WithLegacyKBAdminMetadata(ctx,
		ds.KnowledgeBaseID)).Transaction(func(tx *gorm.DB) error {
		// Same lock order as execution claim/heartbeat/publication.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&after, "id = ?", ds.ID).Error; err != nil {
			return err
		}
		if !sourceMayRenewConsent(&after) || string(after.Config) != string(ds.Config) {
			return errSourceConsentPending
		}
		var run types.SyncLog
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"id = ? AND data_source_id = ? AND tenant_id = ? AND status = ? AND "+
				"execution_claimed = ? AND execution_generation = ? AND "+
				"COALESCE(updated_at,started_at) >= ?",
			execution.SyncLogID, ds.ID, ds.TenantID, types.SyncLogStatusRunning, true, execution.Generation,
			time.Now().UTC().Add(-types.SyncExecutionStaleAfter)).First(&run).Error; err != nil {
			return types.ErrStaleSyncExecution
		}
		current, _, err := loadSourceConsent(tx, &after, ref)
		if err != nil {
			return err
		}
		if current.Epoch != record.Epoch {
			return errSourceConsentPending
		}
		if err := lockBatchPublicationAuthority(tx, &types.FeishuBatch{
			OwnerID:  record.UserID,
			TenantID: ds.TenantID, KBID: ds.KnowledgeBaseID,
		}); err != nil {
			return err
		}
		if err := validatePersonalSourceBinding(tx, &types.FeishuSourceReference{
			Kind:        types.FeishuUserOAuth,
			OwnerUserID: record.UserID, AppID: record.AppID, ConnectionID: record.ConnectionID,
		}); err != nil {
			return err
		}
		if err := s.assertConnectionHealth(tx, record.UserID); err != nil {
			return err
		}
		var connection types.FeishuConnection
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
			"user_id = ? AND app_id = ? AND open_id = ?", record.UserID, record.AppID, record.OpenID,
		).First(&connection).Error; err != nil {
			return err
		}
		var tokens feishuTokens
		if openFeishu(connection.Tokens, &tokens) != nil || !rebindScopesValid(tokens.Scope, r.Type) {
			return core.ErrUserScope
		}
		if err := checkSourceRebindAuthority(tx, ds.ID, ref.ConnectionID, item.OldEpoch, time.Now()); err != nil {
			return err
		}
		audience, err := syncPolicyAudience(tx, ds.KnowledgeBaseID)
		if err != nil {
			return err
		}
		if err := feishusafety.Check(ctx); err != nil {
			return err
		}
		next, err := patchSourceConnection(after.Config, record.ConnectionID)
		if err != nil {
			return err
		}
		updated := tx.Model(&types.DataSource{}).Where(
			"id = ? AND config = ? AND legacy_access_state = ? AND status = ?",
			ds.ID, string(ds.Config), "active", ds.Status,
		).UpdateColumn("config", next)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrFeishuRebindConflict
		}
		if err := tx.First(&after, "id = ?", ds.ID).Error; err != nil {
			return err
		}
		receipt := sourceConsentReceipt{
			ID: uuid.NewString(), Epoch: record.Epoch, UserID: record.UserID,
			AppID: record.AppID, OpenID: record.OpenID,
			ConfigVersion: record.ConfigVersion, ConnectionID: record.ConnectionID, SourceID: ds.ID,
			KBID: ds.KnowledgeBaseID, TenantID: ds.TenantID,
			KnowledgeID: imported.KnowledgeID, ConfigHash: syncPolicyHash(after.Config), Audience: audience,
			CreatedAt: time.Now().UTC(),
		}
		if err := createRebindSetting(tx, sourceRebindPrefix+"auto."+receipt.ID, record.UserID, receipt); err != nil {
			return err
		}
		return writeRebindMarker(tx, ds.ID, record.UserID,
			sourceBindingMarker{ConnectionID: record.ConnectionID, ConsentReceipt: receipt.ID})
	})
	if err == nil {
		ds.Config = after.Config
	}
	return err
}

func checkConsentBindingReceipt(tx *gorm.DB, sourceID string, marker sourceBindingMarker, priorEpoch string) error {
	var receipt sourceConsentReceipt
	if err := readSourceSetting(tx, sourceRebindPrefix+"auto."+marker.ConsentReceipt, &receipt); err != nil {
		return err
	}
	if receipt.ID != marker.ConsentReceipt || receipt.SourceID != sourceID ||
		receipt.ConnectionID != marker.ConnectionID || marker.GrantID != "" ||
		receipt.ConnectionID != connectionID(types.FeishuUserOAuth, receipt.UserID, receipt.AppID,
			receipt.OpenID, receipt.Epoch) {
		return ErrFeishuSharingApproval
	}
	var ds types.DataSource
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
		"id = ? AND tenant_id = ? AND knowledge_base_id = ?", sourceID, receipt.TenantID, receipt.KBID,
	).First(&ds).Error; err != nil {
		return err
	}
	if !sourceMayRenewConsent(&ds) || syncPolicyHash(ds.Config) != receipt.ConfigHash {
		return ErrFeishuSharingApproval
	}
	var imported int64
	if err := tx.Model(&types.FeishuPersonalImport{}).Where(
		"id = ? AND user_id = ? AND app_id = ? AND open_id = ? AND kb_id = ? AND "+
			"knowledge_id = ? AND auth_mode = ?",
		ds.ID, receipt.UserID, receipt.AppID, receipt.OpenID, receipt.KBID, receipt.KnowledgeID,
		types.FeishuUserOAuth).Count(&imported).Error; err != nil {
		return err
	}
	if imported != 1 {
		return ErrFeishuSharingApproval
	}
	if priorEpoch != "" {
		if priorEpoch != receipt.Epoch {
			return ErrFeishuSharingApproval
		}
	} else {
		if err := validatePersonalSourceBinding(tx, &types.FeishuSourceReference{
			Kind:        types.FeishuUserOAuth,
			OwnerUserID: receipt.UserID, AppID: receipt.AppID, ConnectionID: receipt.ConnectionID,
		}); err != nil {
			return err
		}
	}
	audience, err := syncPolicyAudience(tx, receipt.KBID)
	if err != nil {
		return err
	}
	if audience != receipt.Audience {
		return ErrFeishuSharingApproval
	}
	return nil
}

func patchSourceConnection(original types.JSON, connection string) (types.JSON, error) {
	var config map[string]json.RawMessage
	if json.Unmarshal(original, &config) != nil {
		return nil, ErrFeishuSourceRebindRequired
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(config["settings"], &settings) != nil {
		return nil, ErrFeishuSourceRebindRequired
	}
	var ref types.FeishuSourceReference
	if json.Unmarshal(settings["credential_reference"], &ref) != nil {
		return nil, ErrFeishuSourceRebindRequired
	}
	ref.ConnectionID = connection
	settings["credential_reference"], _ = json.Marshal(ref)
	config["settings"], _ = json.Marshal(settings)
	next, err := json.Marshal(config)
	return types.JSON(next), err
}
