package service

import (
	"context"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

func personalHandoffApproved(importID string) bool {
	if importID == "" {
		return false
	}
	for _, id := range strings.Split(os.Getenv("FEISHU_PERSONAL_UNPUBLISHED_HANDOFF_IDS"), ",") {
		if strings.TrimSpace(id) == importID {
			return true
		}
	}
	return false
}

func (p *PersonalRecovery) validatePersonalHandoffTx(tx *gorm.DB, old, incoming *types.FeishuPersonalImport, a *types.FeishuPersonalAttempt) error {
	if !personalHandoffApproved(old.ID) || old.UserID != incoming.UserID {
		return repository.ErrPersonalAuthModeConflict
	}
	// Live local authority is checked under the admission transaction, including
	// private-owner, membership and KB write restrictions. No new grant is minted.
	return p.service.requireImportWriter(tx, incoming.UserID, incoming.KBID, a.CallerTenantID)
}

// This proof is called by ordinary user admission using only that user's token.
// No operator token, application fallback or request-supplied authority exists.
func (p *PersonalRecovery) preflightPersonalHandoff(ctx context.Context, m *types.FeishuPersonalImport, resource core.PersonalResource) error {
	if m.AuthMode != types.FeishuUserOAuth || os.Getenv("FEISHU_PERSONAL_UNPUBLISHED_HANDOFF_IDS") == "" {
		return nil
	}
	var prior []types.FeishuPersonalImport
	if err := p.service.db.WithContext(ctx).Where("user_id=? AND app_id=? AND kb_id=? AND resource_type=? AND document_token=? AND auth_mode=?",
		m.UserID, m.AppID, m.KBID, m.ResourceType, m.DocumentToken, types.FeishuApplicationGrant).Limit(2).Find(&prior).Error; err != nil {
		return err
	}
	if len(prior) == 0 {
		return nil
	}
	if len(prior) != 1 || !personalHandoffApproved(prior[0].ID) {
		return repository.ErrPersonalAuthModeConflict
	}
	client := p.service.client(&core.Config{AppID: m.AppID})
	rights, err := client.InspectPersonalPermissions(ctx, resource)
	if err != nil {
		return err
	}
	if !rights.View {
		return core.ErrPersonalReadDenied
	}
	if !rights.Copy {
		return core.ErrPersonalCopyDenied
	}
	if !rights.Export {
		return core.ErrPersonalCopyOnlyReview
	}
	latest, err := client.ResolvePersonalResource(ctx, resource)
	if err != nil {
		return err
	}
	if latest.Type != resource.Type || latest.Token != resource.Token || latest.Modified != resource.Modified {
		return repository.ErrPersonalIdentity
	}
	return nil
}
