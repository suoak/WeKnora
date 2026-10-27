package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// ErrFeishuTargetRequired rejects implicit destination creation or substitution.
var ErrFeishuTargetRequired = errors.New("feishu_target_knowledge_base_required")

// FeishuImportTarget exposes only destinations with a fresh editor decision.
type FeishuImportTarget struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	RequiresSharingApproval bool   `json:"requires_sharing_approval"`
}

// ImportTargets lists owned and organization-shared existing writable KBs.
func (s *FeishuPersonalService) ImportTargets(ctx context.Context) ([]FeishuImportTarget, error) {
	caller := types.CallerFromContext(ctx)
	if !caller.Role.HasPermission(types.TenantRoleContributor) {
		return []FeishuImportTarget{}, nil
	}
	kbs, err := s.kbs.ListKnowledgeBases(ctx)
	if err != nil {
		return nil, err
	}
	if s.recovery == nil {
		return nil, ErrFeishuStagingRequired
	}
	if s.recovery.shares != nil {
		shared, e := s.recovery.shares.ListSharedKnowledgeBases(ctx, caller.TenantID, caller.Role)
		if e != nil {
			return nil, e
		}
		for _, row := range shared {
			if row != nil && row.KnowledgeBase != nil {
				kbs = append(kbs, row.KnowledgeBase)
			}
		}
	}
	result := []FeishuImportTarget{}
	seen := map[string]bool{}
	for _, kb := range kbs {
		if kb == nil || seen[kb.ID] || kb.Type == types.KnowledgeBaseTypeFAQ || kb.IsTemporary {
			continue
		}
		if _, e := access.ResolveKB(ctx, access.KBRequest{Caller: caller}, kb, types.OrgRoleEditor,
			s.recovery.shares, nil); e != nil {
			continue
		}
		seen[kb.ID] = true
		result = append(
			result,
			FeishuImportTarget{
				ID:                      kb.ID,
				Name:                    kb.Name,
				RequiresSharingApproval: false,
			},
		)
	}
	return result, nil
}

// CreatePrivateKnowledgeBase creates a new KB with an atomic privacy marker.
func (s *FeishuPersonalService) CreatePrivateKnowledgeBase(
	ctx context.Context,
	name string,
) (*types.KnowledgeBase, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok || types.IsSyntheticUserID(user) {
		return nil, access.ErrUnauthorized
	}
	if !types.CallerFromContext(ctx).Role.HasPermission(types.TenantRoleContributor) {
		return nil, access.ErrForbidden
	}
	cfg, err := s.app(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Capabilities == nil || !cfg.modeEnabled(types.FeishuUserOAuth) ||
		!types.PrivateKBGuardsEnabled(s.db) {
		return nil, errors.New("private_knowledge_base_unavailable")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 255 {
		return nil, errors.New("invalid_knowledge_base_name")
	}
	// Only new, empty KBs can become private; existing public data never changes
	// visibility as a side effect of enabling Feishu.
	kb, err := s.kbs.CreateKnowledgeBase(
		types.WithPrivateKBCreation(ctx, user),
		&types.KnowledgeBase{Name: name, Type: types.KnowledgeBaseTypeDocument},
	)
	if err != nil {
		return nil, err
	}
	kb.PrivateOwnerID = user
	return kb, nil
}

// Private workers reconstruct the persisted owner after live user/member/KB
// checks. An absent task caller never gets a tenant-wide privacy bypass.
func (p *PersonalRecovery) privateWorkerContext(
	ctx context.Context,
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
) (context.Context, error) {
	owner, err := types.PrivateKBOwner(p.service.db.WithContext(ctx), m.KBID)
	if err != nil {
		return ctx, err
	}
	if owner == "" {
		return ctx, nil
	}
	if owner != m.UserID {
		return ctx, access.ErrForbidden
	}
	fresh, _, err := p.authorizeLocal(ctx, m, a)
	return fresh, err
}

// EnsurePrivateKnowledgeBase serializes first-use creation per tenant and human
// owner. Only an atomic private marker plus the existing access policy proves privacy.
func (s *FeishuPersonalService) EnsurePrivateKnowledgeBase(
	ctx context.Context,
) (*types.KnowledgeBase, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok || types.IsSyntheticUserID(user) {
		return nil, access.ErrUnauthorized
	}
	if s.db.Name() != "postgres" || !types.PrivateKBGuardsEnabled(s.db) {
		return nil, errors.New("private_knowledge_base_unavailable")
	}
	tenant := types.CallerFromContext(ctx).TenantID
	var result *types.KnowledgeBase
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		key := fmt.Sprintf("feishu-private-kb:%d:%s", tenant, user)
		if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; e != nil {
			return e
		}
		var ids []string
		if e := tx.Table("feishu_private_kbs AS fp").Select("fp.kb_id").
			Joins("JOIN knowledge_bases kb ON kb.id = fp.kb_id").
			Where("fp.owner_id = ? AND kb.tenant_id = ? AND kb.creator_id = ? AND kb.deleted_at IS NULL",
				user, tenant, user).
			Order("fp.kb_id").Limit(100).Pluck("fp.kb_id", &ids).Error; e != nil {
			return e
		}
		for _, id := range ids {
			kb, e := s.kbs.GetKnowledgeBaseByID(ctx, id)
			if e == nil && kb.PrivateOwnerID == user && kb.CreatorID == user && kb.TenantID == tenant {
				result = kb
				return nil
			}
		}
		var e error
		result, e = s.CreatePrivateKnowledgeBase(ctx, "我的飞书知识")
		return e
	})
	return result, err
}

// QuickImport binds the personal identity without offering a higher privilege
// fallback. A supplied shared destination still requires per-resource approval.
func (s *FeishuPersonalService) QuickImport(
	ctx context.Context,
	kb, key string,
	links []string,
	publication ...bool,
) (*FeishuBatchView, error) {
	if strings.TrimSpace(kb) == "" {
		return nil, ErrFeishuTargetRequired
	}
	if len(links) < 1 || len(links) > 100 || len(key) < 1 || len(key) > 128 {
		return nil, errors.New("invalid_batch_limits")
	}
	if !types.CallerFromContext(ctx).Role.HasPermission(types.TenantRoleContributor) {
		return nil, access.ErrForbidden
	}
	_, _, ref, err := s.ResolveCredentialReference(ctx, types.FeishuUserOAuth, "")
	if err != nil {
		return nil, err
	}
	base, err := s.kbs.GetKnowledgeBaseByID(ctx, kb)
	if err != nil {
		return nil, err
	}
	if s.recovery == nil {
		return nil, ErrFeishuStagingRequired
	}
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: types.CallerFromContext(ctx)},
		base, types.OrgRoleEditor, s.recovery.shares, nil)
	if err != nil {
		return nil, err
	}
	ctx = grant.Context(ctx)
	return s.CreateBatch(ctx, kb, types.FeishuUserOAuth, ref.ConnectionID, key, links, publication...)
}
