package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

func canRetryPersonalAcquisition(m *types.FeishuPersonalImport, a *types.FeishuPersonalAttempt) bool {
	if m == nil || a == nil || m.KnowledgeID != "" || m.Status != "needs_attention" ||
		m.AuthMode != types.FeishuUserOAuth || a.AuthMode != types.FeishuUserOAuth ||
		a.Status != "needs_attention" || a.OwnerNonce != "" || a.LeaseUntil != nil ||
		a.ExpectedActiveVersion != "" {
		return false
	}
	switch a.LastErrorCategory {
	case "temporary_failure", "retry_exhausted", "storage_unavailable", "storage_permission_denied",
		"authorization_revalidation_failed", "authorization_expired", "authorization_invalid",
		"authorization_or_resource_missing", "reconnect_required", "refresh_token_expired", "refresh_rejected",
		"user_scope_missing", "source_read_denied", "source_copy_denied", "source_copy_export_denied",
		"source_permission_required", "source_metadata_denied", "export_permission_required",
		"export_create_denied", "export_download_denied", "export_status_denied", "source_copy_only_requires_policy":
		return true
	default:
		return false
	}
}

func (s *FeishuPersonalService) retryPersonalBatchItem(
	ctx context.Context, batch *types.FeishuBatch, item *types.FeishuBatchItem, ref types.FeishuCredentialRef,
) error {
	if item.KnowledgeID != "" {
		return s.retryExactBatchItem(ctx, batch, item, ref)
	}
	if s.recovery == nil || !s.recovery.firstCreateOnly {
		return ErrFeishuBatchNotRetryable
	}
	var imported types.FeishuPersonalImport
	if err := s.db.WithContext(ctx).Where("id=? AND user_id=? AND kb_id=?",
		item.ImportID, batch.OwnerID, batch.KBID).Take(&imported).Error; err != nil {
		return err
	}
	var resource types.FeishuPersonalResource
	if err := s.db.WithContext(ctx).Where("import_id=?", imported.ID).Take(&resource).Error; err != nil {
		return err
	}
	_, observedResource, attempt, err := s.recovery.repo.Load(ctx, resource.CurrentAttemptID)
	if err != nil {
		return err
	}
	if !canRetryPersonalAcquisition(&imported, attempt) || attempt.CallerTenantID != batch.TenantID ||
		attempt.AppConfigVersion != ref.ConfigVersion || imported.UserID != ref.OwnerID ||
		imported.AppID != ref.AppID || imported.OpenID != ref.OpenID {
		return ErrFeishuBatchNotRetryable
	}
	return s.recovery.repo.QueuePersonalReadmission(ctx, attempt, observedResource, batch.ID, item.ID,
		func(tx *gorm.DB, current *types.FeishuPersonalImport, a *types.FeishuPersonalAttempt) error {
			if !canRetryPersonalAcquisition(current, a) {
				return repository.ErrPersonalFence
			}
			if err := s.requireImportWriter(tx, current.UserID, current.KBID, a.CallerTenantID); err != nil {
				return err
			}
			return s.lockPersonalRetryCredential(tx, ref)
		})
}
