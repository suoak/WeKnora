package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The owner explicitly accepts the currently connected identity. A batch handle
// is not authority to change the owner, app, destination or existing imports.
func (s *FeishuPersonalService) bindBatchRetryConsent(
	ctx context.Context, batch *types.FeishuBatch, ref types.FeishuCredentialRef,
) error {
	if batch.ConnectionID == ref.ConnectionID {
		return nil
	}
	if batch.AuthMode != types.FeishuUserOAuth || ref.Kind != types.FeishuUserOAuth ||
		batch.OwnerID != ref.OwnerID || batch.ConfigVersion != ref.ConfigVersion {
		return access.ErrForbidden
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.FeishuBatch
		batchQuery := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"id=? AND owner_id=? AND tenant_id=?", batch.ID, batch.OwnerID, batch.TenantID)
		if err := batchQuery.Take(&current).Error; err != nil {
			return err
		}
		if current.ConnectionID != batch.ConnectionID || current.ConfigVersion != batch.ConfigVersion ||
			current.KBID != batch.KBID || current.AuthMode != batch.AuthMode {
			return repository.ErrPersonalFence
		}
		if err := s.requireImportWriter(tx, batch.OwnerID, batch.KBID, batch.TenantID); err != nil {
			return err
		}
		if err := s.lockPersonalRetryCredential(tx, ref); err != nil {
			return err
		}
		var imports []types.FeishuPersonalImport
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
			"id IN (SELECT import_id FROM feishu_import_batch_items WHERE batch_id=?)", batch.ID).
			Find(&imports).Error; err != nil {
			return err
		}
		var expected int64
		if err := tx.Model(&types.FeishuBatchItem{}).Where("batch_id=? AND import_id<>''", batch.ID).
			Distinct("import_id").Count(&expected).Error; err != nil {
			return err
		}
		if expected != int64(len(imports)) {
			return repository.ErrPersonalIdentity
		}
		for _, m := range imports {
			if m.UserID != batch.OwnerID || m.KBID != batch.KBID || m.AuthMode != types.FeishuUserOAuth ||
				m.AppID != ref.AppID || m.OpenID != ref.OpenID {
				return repository.ErrPersonalIdentity
			}
		}
		body, err := json.Marshal(map[string]any{
			"batch_id": batch.ID, "owner_id": batch.OwnerID,
			"previous_connection": current.ConnectionID, "current_connection": ref.ConnectionID,
		})
		if err != nil {
			return err
		}
		if err := tx.Create(&types.TaskDeadLetter{
			TenantID: batch.TenantID, TaskType: "feishu:batch_retry_consent",
			Scope: "feishu_batch", ScopeID: batch.ID, Payload: body, FailedAt: time.Now(),
			LastError: "explicit_owner_reauthorization_before_retry",
		}).Error; err != nil {
			return err
		}
		return tx.Model(&current).Update("connection_id", ref.ConnectionID).Error
	})
}

// Called under the existing resume/batch transaction. SHARE locks fence local
// membership, KB, app configuration, connection and consent revocation races.
// This never refreshes a token or makes a network call under SQL locks.
func (s *FeishuPersonalService) lockPersonalRetryCredential(tx *gorm.DB, ref types.FeishuCredentialRef) error {
	var setting types.SystemSetting
	settingQuery := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("key=?", feishuAppSetting)
	if err := settingQuery.Take(&setting).Error; err != nil {
		return err
	}
	cipher, err := setting.AsString()
	if err != nil {
		return err
	}
	var cfg FeishuAppConfig
	if err := openFeishu(cipher, &cfg); err != nil {
		return err
	}
	if !cfg.Enabled || !cfg.modeEnabled(types.FeishuUserOAuth) || cfg.AppID != ref.AppID ||
		cfg.identityVersion(types.FeishuUserOAuth) != ref.ConfigVersion {
		return repository.ErrPersonalIdentity
	}
	var connection types.FeishuConnection
	connectionQuery := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
		"user_id=? AND app_id=? AND open_id=?", ref.OwnerID, ref.AppID, ref.OpenID)
	if err := connectionQuery.Take(&connection).Error; err != nil {
		return ErrFeishuReconnect
	}
	var epoch types.FeishuPersonalGrantEpoch
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
		"user_id=? AND epoch=?", ref.OwnerID, ref.GrantEpoch).Take(&epoch).Error; err != nil {
		return ErrFeishuReconnect
	}
	return s.assertConnectionHealth(tx, ref.OwnerID)
}

func (s *FeishuPersonalService) retryExactBatchItem(
	ctx context.Context, batch *types.FeishuBatch, item *types.FeishuBatchItem, ref types.FeishuCredentialRef,
) error {
	if s.recovery == nil || item.ImportID == "" || batch.AuthMode != types.FeishuUserOAuth {
		return ErrFeishuBatchNotRetryable
	}
	p := s.recovery
	var m types.FeishuPersonalImport
	if err := s.db.WithContext(ctx).Where("id=? AND user_id=? AND kb_id=?", item.ImportID, batch.OwnerID, batch.KBID).
		Take(&m).Error; err != nil {
		return err
	}
	if m.Status != "needs_attention" || m.KnowledgeID == "" || m.AuthMode != types.FeishuUserOAuth ||
		m.AppID != ref.AppID || m.OpenID != ref.OpenID || m.UserID != ref.OwnerID {
		return repository.ErrPersonalIdentity
	}
	var sources int64
	if err := s.db.WithContext(ctx).Model(&types.DataSource{}).Where("id=?", m.ID).Count(&sources).Error; err != nil {
		return err
	}
	if sources != 0 {
		return repository.ErrPersonalSourceManaged
	}
	var resource types.FeishuPersonalResource
	if err := s.db.WithContext(ctx).Where("import_id=?", m.ID).Take(&resource).Error; err != nil {
		return err
	}
	_, _, observed, err := p.repo.Load(ctx, resource.CurrentAttemptID)
	if err != nil {
		return err
	}
	if observed.CallerTenantID != batch.TenantID || observed.ImportID != m.ID ||
		observed.AppConfigVersion != ref.ConfigVersion || !p.canResumePersonalExact(observed) {
		return ErrFeishuBatchNotRetryable
	}
	// A completed export may already be a workspace-owned immutable snapshot.
	// That protocol deliberately survives source OAuth rotation; do not rewrite it.
	snapshot, err := repository.CheckWorkspaceImportSnapshot(ctx, s.db, &m, observed, false)
	if err != nil {
		return err
	}
	epoch := observed.GrantEpoch
	check := &personalExactPreflight{observed: observed, closed: true}
	if !snapshot && ref.GrantEpoch != observed.GrantEpoch {
		epoch = ref.GrantEpoch
		check.renewedEpoch = epoch
	}
	if _, err := p.checkExactPersonalSource(ctx, observed, false, check); err != nil {
		return err
	}
	if check.source == nil || check.validate == nil {
		return errPersonalExactDocument
	}
	validate := func(tx *gorm.DB, a *types.FeishuPersonalAttempt) error {
		if err := s.requireImportWriter(tx, m.UserID, m.KBID, a.CallerTenantID); err != nil {
			return err
		}
		if err := s.lockPersonalRetryCredential(tx, ref); err != nil {
			return err
		}
		var setting types.SystemSetting
		if err := tx.Where("key=?", feishuAppSetting).Take(&setting).Error; err != nil {
			return err
		}
		cipher, err := setting.AsString()
		if err != nil {
			return err
		}
		var cfg FeishuAppConfig
		if err := openFeishu(cipher, &cfg); err != nil {
			return err
		}
		if err := s.checkSharingApproval(tx, &cfg, &m, a.Revision); err != nil {
			return err
		}
		return check.validate(tx, a)
	}
	source := exactSourceReceipt(check.source)
	if err := p.repo.QueueExactResumeWithConsent(ctx, observed, source, validate, epoch); err != nil {
		return err
	}
	ks, ok := s.knowledge.(*knowledgeService)
	if !ok {
		return ErrFeishuStagingRequired
	}
	dispatch, ok := ks.repo.(interfaces.KnowledgeDispatchRepository)
	if !ok {
		return ErrFeishuStagingRequired
	}
	op, err := dispatch.GetKnowledgeDispatch(ctx, observed.CallerTenantID, observed.LogicalKnowledgeID)
	if err != nil {
		return err
	}
	return ks.publishKnowledgeDispatch(ctx, op)
}

func retryBatchWebOwner(ctx context.Context, owner string) error {
	principal, ok := types.PrincipalFromContext(ctx)
	user, hasUser := types.UserIDFromContext(ctx)
	if !ok || !hasUser || user != owner || principal.Type != types.PrincipalWebUser || principal.ID != owner {
		return access.ErrForbidden
	}
	return nil
}
