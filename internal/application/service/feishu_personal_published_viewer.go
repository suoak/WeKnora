package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func publishedViewerDenied(reason string, cause error) error {
	return fmt.Errorf("published_viewer_%s: %w", reason, errors.Join(access.ErrForbidden, cause))
}

// Every read boundary checks the reader, never the original importer's account.
func (p *PersonalRecovery) checkExactPublishedViewer(
	ctx context.Context,
	receipt types.PersonalExactReceipt,
) (*types.KnowledgeBase, error) {
	caller := types.CallerFromContext(ctx)
	var member types.TenantMember
	if err := p.service.db.WithContext(ctx).Where(
		"user_id=? AND tenant_id=? AND status=? AND deleted_at IS NULL",
		caller.UserID, caller.TenantID, types.TenantMemberStatusActive,
	).Take(&member).Error; err != nil {
		return nil, publishedViewerDenied("membership_lookup", err)
	}
	if !member.Role.IsValid() || member.Role != caller.Role {
		return nil, publishedViewerDenied("membership_role", nil)
	}
	var active bool
	if err := p.service.db.WithContext(ctx).Raw(`SELECT
EXISTS(SELECT 1 FROM users WHERE id=? AND is_active AND deleted_at IS NULL) AND
EXISTS(SELECT 1 FROM tenants WHERE id=? AND status='active' AND deleted_at IS NULL) AND
EXISTS(SELECT 1 FROM tenants WHERE id=? AND status='active' AND deleted_at IS NULL)`,
		caller.UserID, caller.TenantID, receipt.TenantID).Scan(&active).Error; err != nil {
		return nil, publishedViewerDenied("account_space_lookup", err)
	}
	if !active {
		return nil, publishedViewerDenied("account_or_space_inactive", nil)
	}
	if scope, keyed := types.TenantAPIKeyScopeFromContext(ctx); keyed {
		if scope.KeyID == 0 || scope.ScopeType != types.APIKeyScopeUserMCP || scope.FullAccess ||
			!scope.HasCapability(types.APIKeyCapabilityRetrieve) {
			return nil, access.ErrForbidden
		}
		keys, ok := repository.NewTenantAPIKeyRepository(p.service.db).(interfaces.UserMCPAPIKeyRepository)
		if !ok {
			return nil, access.ErrForbidden
		}
		key, err := keys.GetUserMCPAPIKey(ctx, caller.UserID, scope.KeyID)
		if err != nil || key.OwnerUserID == nil || *key.OwnerUserID != caller.UserID || key.FullAccess ||
			key.RevokedAt != nil || (key.ExpiresAt != nil && !time.Now().Before(*key.ExpiresAt)) ||
			!(types.TenantAPIKeyScope{Capabilities: key.Capabilities}).HasCapability(types.APIKeyCapabilityRetrieve) {
			return nil, access.ErrUnauthorized
		}
		live, err := keys.GetUserMCPTenantScope(ctx, key.ID, caller.TenantID)
		if err != nil || live == nil || live.APIKeyID != key.ID || live.TenantID != caller.TenantID ||
			(live.KBScopeMode != types.APIKeyKBScopeAll && live.KBScopeMode != types.APIKeyKBScopeSelected) {
			return nil, access.ErrForbidden
		}
		scope.KnowledgeBaseRestricted = live.KBScopeMode == types.APIKeyKBScopeSelected
		scope.KnowledgeBaseIDs = nil
		for _, entry := range live.KnowledgeBases {
			if entry.APIKeyID != key.ID || entry.TenantID != caller.TenantID ||
				(entry.SourceType != types.APIKeyKBSourceOwned && entry.SourceType != types.APIKeyKBSourceShared) ||
				(entry.SourceType == types.APIKeyKBSourceShared &&
					(entry.KBShareID == nil || *entry.KBShareID == "")) ||
				(entry.KnowledgeBaseID == receipt.KnowledgeBaseID &&
					entry.SourceType == types.APIKeyKBSourceOwned && receipt.TenantID != caller.TenantID) {
				return nil, access.ErrForbidden
			}
			scope.KnowledgeBaseIDs = append(scope.KnowledgeBaseIDs, entry.KnowledgeBaseID)
		}
		ctx = types.WithTenantAPIKeyScope(ctx, scope)
	}
	kb, err := p.loadAuthorizationKB(ctx, receipt.KnowledgeBaseID)
	if err != nil {
		return nil, publishedViewerDenied("kb_lookup", err)
	}
	if kb.TenantID != receipt.TenantID || kb.IsTemporary {
		return nil, publishedViewerDenied("kb_binding", nil)
	}
	if _, err = access.ResolveKB(ctx, access.KBRequest{Caller: caller},
		kb, types.OrgRoleViewer, p.shares, nil); err != nil {
		return nil, err
	}
	return kb, nil
}
