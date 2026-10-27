package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FeishuSyncPublicationPolicy is explicit continuing publication authorization,
// separate from the owner's revision-bound first-import approval. No policy is
// created by import, sync, or a writable-KB check. It authorizes one existing
// personal source in its original destination and never grants source access.
type FeishuSyncPublicationPolicy struct {
	ID, SourceID, OwnerID, KBID, ConnectionID, AppID, ConfigVersion string
	ResourceType, ResourceToken, SourceConfigHash, AudienceHash     string
	InitialApprovalID, AuthorizationRef, Operator, State            string
	GrantEpoch                                                      string
	ReconciliationLogID                                             string
	ReconciliationExpiresAt                                         time.Time
	TenantID                                                        uint64
	CreatedAt, ExpiresAt                                            time.Time
}

const feishuSyncPolicyPrefix = "integration.feishu.sync_publication."

func syncPolicyHash(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func syncPolicyAudience(tx *gorm.DB, kbID string) (string, error) {
	var kb types.KnowledgeBase
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", kbID).First(&kb).Error; err != nil {
		return "", err
	}
	var shares []types.KnowledgeBaseShare
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("knowledge_base_id = ?", kbID).Order("id").Find(&shares).Error; err != nil {
		return "", err
	}
	owner, err := types.PrivateKBOwner(tx, kbID)
	if err != nil {
		return "", err
	}
	value := []any{kb.ID, kb.TenantID, owner}
	for _, share := range shares {
		value = append(
			value,
			[]any{share.ID, share.OrganizationID, share.SourceTenantID, share.Permission},
		)
	}
	encoded, err := json.Marshal(value)
	return syncPolicyHash(encoded), err
}

// ApproveSourceUpdates is an operator/admin enrollment, never an inferred user
// click. The caller must supply an audit reference for the continuing grant.
// Existing exact-version approval, live owner authority and identity are required.
// The setting is encrypted, immutable on enrollment, and read under publication
// transaction locks; it is absent by default and expires within 90 days.
func (s *FeishuPersonalService) ApproveSourceUpdates(
	ctx context.Context, sourceID, operator, authorizationRef string, expires time.Time,
) (*FeishuSyncPublicationPolicy, error) {
	return s.approveSourceUpdates(ctx, sourceID, operator, authorizationRef, expires, "")
}

// ApproveSourceReconciliation prepares a single, explicitly approved full run for
// a held personal source. It never clears hold or enqueues work. The normal sync
// fence, permission checks, staging, indexes and full-reconciliation publication
// must succeed before the existing repository can release the source's hold.
func (s *FeishuPersonalService) ApproveSourceReconciliation(
	ctx context.Context, sourceID, operator, authorizationRef string, expires time.Time,
) (*FeishuSyncPublicationPolicy, error) {
	return s.approveSourceUpdates(
		ctx,
		sourceID,
		operator,
		authorizationRef,
		expires,
		uuid.NewString(),
	)
}

// RenewSourceReconciliation archives an exhausted single-run grant and creates
// a fresh run atomically. It never revives a terminal execution or releases hold.
func (s *FeishuPersonalService) RenewSourceReconciliation(
	ctx context.Context, sourceID, operator, authorizationRef string, expires time.Time,
) (*FeishuSyncPublicationPolicy, error) {
	var next *FeishuSyncPublicationPolicy
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireRebindOperator(ctx, tx, operator); err != nil {
			return err
		}
		var old types.SystemSetting
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("key = ?", feishuSyncPolicyPrefix+sourceID).First(&old).Error; err != nil {
			return err
		}
		cipher, err := old.AsString()
		if err != nil {
			return err
		}
		var previous FeishuSyncPublicationPolicy
		if openFeishu(cipher, &previous) != nil || previous.SourceID != sourceID ||
			previous.State != "approved" || previous.ReconciliationLogID == "" || authorizationRef == "" {
			return access.ErrForbidden
		}
		var run types.SyncLog
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("id = ? AND data_source_id = ?", previous.ReconciliationLogID, sourceID).
			First(&run).
			Error; err != nil {
			return err
		}
		if run.ExecutionClaimed ||
			run.Status != types.SyncLogStatusFailed && run.Status != types.SyncLogStatusCanceled {
			return access.ErrForbidden
		}
		archived := old
		archived.ID = 0
		archived.Key = feishuSyncPolicyPrefix + "archive." + uuid.NewString()
		if err := tx.Create(&archived).Error; err != nil {
			return err
		}
		if err := tx.Delete(&old).Error; err != nil {
			return err
		}
		bound := *s
		bound.db = tx
		next, err = bound.ApproveSourceReconciliation(
			ctx,
			sourceID,
			operator,
			authorizationRef,
			expires,
		)
		return err
	})
	return next, err
}

func (s *FeishuPersonalService) approveSourceUpdates(
	ctx context.Context,
	sourceID, operator, authorizationRef string,
	expires time.Time,
	reconciliationLog string,
) (*FeishuSyncPublicationPolicy, error) {
	admin, _ := ctx.Value(types.SystemAdminContextKey).(bool)
	if !admin || operator == "" || authorizationRef == "" || !expires.After(time.Now()) ||
		expires.After(time.Now().Add(90*24*time.Hour)) {
		return nil, access.ErrForbidden
	}
	var policy FeishuSyncPublicationPolicy
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ds types.DataSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", sourceID).First(&ds).Error; err != nil {
			return err
		}
		if !legacyWikiDrive(ds.Type) || ds.LegacyAccessState == "revoked" ||
			(ds.LegacyAccessState == "hold") != (reconciliationLog != "") ||
			ds.Status != types.DataSourceStatusActive && ds.Status != types.DataSourceStatusError {
			return access.ErrForbidden
		}
		config, err := ds.ParseConfig()
		if err != nil {
			return err
		}
		ref, err := types.ParseFeishuSourceReference(config)
		if err != nil || ref == nil || ref.Kind != types.FeishuUserOAuth {
			return access.ErrForbidden
		}
		if err := validatePersonalSourceBinding(tx, ref); err != nil {
			return err
		}
		if err := checkRebindPublication(tx, ds.ID, ref.ConnectionID); err != nil {
			return err
		}
		var epoch types.FeishuPersonalGrantEpoch
		if err = tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("user_id = ?", ref.OwnerUserID).First(&epoch).Error; err != nil {
			return ErrFeishuReconnect
		}
		links, reference, err := core.ReferenceLinks(config)
		if err != nil || !reference || len(links) != 1 {
			return access.ErrForbidden
		}
		if err = lockBatchPublicationAuthority(tx, &types.FeishuBatch{
			OwnerID: ref.OwnerUserID, TenantID: ds.TenantID, KBID: ds.KnowledgeBaseID,
		}); err != nil {
			return err
		}
		var imported types.FeishuPersonalImport
		if err = tx.Where("id = ? AND user_id = ? AND kb_id = ? AND app_id = ? AND auth_mode = ?",
			ds.ID, ref.OwnerUserID, ds.KnowledgeBaseID, ref.AppID, types.FeishuUserOAuth).
			First(&imported).Error; err != nil {
			return err
		}
		var initial types.FeishuSharingApproval
		if err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
			"owner_id = ? AND kb_id = ? AND app_id = ? AND resource_type = ? AND resource_token = ?"+
				" AND config_version = ? AND state = ? AND expires_at > ?",
			ref.OwnerUserID, ds.KnowledgeBaseID, ref.AppID, imported.ResourceType, imported.DocumentToken,
			ref.IdentityVersion, "approved", time.Now()).Order("created_at DESC").First(&initial).Error; err != nil {
			return ErrFeishuSharingApproval
		}
		audience, err := syncPolicyAudience(tx, ds.KnowledgeBaseID)
		if err != nil {
			return err
		}
		policy = FeishuSyncPublicationPolicy{
			ID: uuid.NewString(), SourceID: ds.ID, OwnerID: ref.OwnerUserID, KBID: ds.KnowledgeBaseID,
			TenantID: ds.TenantID, ConnectionID: ref.ConnectionID, AppID: ref.AppID, ConfigVersion: ref.IdentityVersion,
			ResourceType: imported.ResourceType, ResourceToken: imported.DocumentToken,
			SourceConfigHash: syncPolicyHash(
				ds.Config,
			), AudienceHash: audience, InitialApprovalID: initial.ID,
			GrantEpoch:       epoch.Epoch,
			AuthorizationRef: authorizationRef, Operator: operator, State: "approved",
			CreatedAt: time.Now(), ExpiresAt: expires,
		}
		if reconciliationLog != "" {
			var running int64
			if err := tx.Model(&types.SyncLog{}).Where("data_source_id = ? AND status = ?",
				ds.ID, types.SyncLogStatusRunning).Count(&running).Error; err != nil {
				return err
			}
			if running != 0 {
				return access.ErrForbidden
			}
			policy.ReconciliationLogID = reconciliationLog
			policy.ReconciliationExpiresAt = minTime(expires, time.Now().Add(15*time.Minute))
			// The grant and run are one transaction. The approved operator can
			// retry enqueue of this same durable run; another log cannot use it.
			if err := tx.Create(&types.SyncLog{
				ID: reconciliationLog, DataSourceID: ds.ID, TenantID: ds.TenantID,
				Status: types.SyncLogStatusRunning, StartedAt: time.Now().UTC(),
			}).Error; err != nil {
				return err
			}
		}
		cipher, err := sealFeishu(policy)
		if err != nil {
			return err
		}
		value, err := json.Marshal(cipher)
		if err != nil {
			return err
		}
		return tx.Create(&types.SystemSetting{
			Key: feishuSyncPolicyPrefix + ds.ID, Value: value, ValueType: "string", Category: "integration",
			Description: "Explicit source-scoped continuing publication policy",
			IsSecret:    true, LastModifiedBy: operator,
		}).Error
	})
	return &policy, err
}

func checkSourcePublicationPolicy(
	tx *gorm.DB,
	ds *types.DataSource,
	ref *types.FeishuSourceReference,
	resource core.PersonalResource,
) error {
	// This exception belongs only to the server-owned sync execution. It cannot
	// approve a new quick-import batch, another resource or a manual identity.
	execution, ok := types.SyncExecutionFromContext(tx.Statement.Context)
	if !ok || execution.DataSourceID != ds.ID || execution.TenantID != ds.TenantID ||
		resource.Modified == "" {
		return ErrFeishuSharingApproval
	}
	if err := validatePersonalSourceBinding(tx, ref); err != nil {
		return errors.Join(ErrFeishuSharingApproval, err)
	}
	var row types.SystemSetting
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("key = ?", feishuSyncPolicyPrefix+ds.ID).
		First(&row).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrFeishuSharingApproval
	}
	if err != nil {
		return err
	}
	cipher, err := row.AsString()
	if err != nil {
		return ErrFeishuSharingApproval
	}
	var policy FeishuSyncPublicationPolicy
	if openFeishu(cipher, &policy) != nil || policy.State != "approved" || policy.ID == "" ||
		policy.AuthorizationRef == "" || policy.Operator == "" || !policy.ExpiresAt.After(time.Now()) ||
		policy.SourceID != ds.ID || policy.KBID != ds.KnowledgeBaseID || policy.TenantID != ds.TenantID ||
		policy.OwnerID != ref.OwnerUserID || policy.AppID != ref.AppID || policy.ConnectionID != ref.ConnectionID ||
		policy.ConfigVersion != ref.IdentityVersion || policy.ResourceType != resource.Type ||
		policy.ResourceToken != resource.Token ||
		policy.SourceConfigHash != syncPolicyHash(ds.Config) || ref.Kind != types.FeishuUserOAuth {
		return ErrFeishuSharingApproval
	}
	var current types.DataSource
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", ds.ID).First(&current).Error; err != nil {
		return err
	}
	if current.Status != types.DataSourceStatusActive &&
		current.Status != types.DataSourceStatusError ||
		current.LegacyAccessState == "revoked" ||
		policy.SourceConfigHash != syncPolicyHash(current.Config) {
		return ErrFeishuSharingApproval
	}
	if current.LegacyAccessState == "hold" {
		if execution.SyncLogID == "" || execution.SyncLogID != policy.ReconciliationLogID ||
			execution.Generation < 1 || !policy.ReconciliationExpiresAt.After(time.Now()) {
			return ErrFeishuSharingApproval
		}
		var run types.SyncLog
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
			"id = ? AND data_source_id = ? AND tenant_id = ? AND status = ?"+
				" AND execution_generation = ? AND execution_claimed = ?",
			execution.SyncLogID, ds.ID, ds.TenantID, types.SyncLogStatusRunning, execution.Generation, true,
		).First(&run).Error; err != nil {
			return ErrFeishuSharingApproval
		}
	}
	var epoch types.FeishuPersonalGrantEpoch
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("user_id = ? AND epoch = ?", policy.OwnerID, policy.GrantEpoch).First(&epoch).Error; err != nil {
		return ErrFeishuSharingApproval
	}
	var initial types.FeishuSharingApproval
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
		"id = ? AND owner_id = ? AND kb_id = ? AND app_id = ? AND resource_type = ? AND resource_token = ?"+
			" AND config_version = ? AND state = ?",
		policy.InitialApprovalID, policy.OwnerID, policy.KBID, policy.AppID, policy.ResourceType, policy.ResourceToken,
		policy.ConfigVersion, "approved").First(&initial).Error; err != nil {
		return ErrFeishuSharingApproval
	}
	// Policy duration is its own explicit grant; revoking the initial approval
	// still invalidates it. The original approval is never extended or rewritten.
	audience, err := syncPolicyAudience(tx, policy.KBID)
	if err != nil || audience != policy.AudienceHash {
		return ErrFeishuSharingApproval
	}
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *FeishuPersonalService) checkHeldSourceReconciliation(
	ctx context.Context, ds *types.DataSource, ref *types.FeishuSourceReference,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var imported types.FeishuPersonalImport
		if err := tx.Where("id = ? AND user_id = ? AND kb_id = ? AND app_id = ? AND auth_mode = ?",
			ds.ID, ref.OwnerUserID, ds.KnowledgeBaseID, ref.AppID, types.FeishuUserOAuth).
			First(&imported).Error; err != nil {
			return ErrFeishuSharingApproval
		}
		return checkSourcePublicationPolicy(tx, ds, ref, core.PersonalResource{
			Type: imported.ResourceType, Token: imported.DocumentToken, Modified: "reconciliation-preflight",
		})
	})
}
