package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

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
