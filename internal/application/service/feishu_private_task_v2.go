package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

// PrivateTaskMiddleware reconstructs only the persisted private KB owner's
// current authority for internal processing tasks. It never changes API reads,
// key principals, transfers/clones, or the independent Feishu recovery fences.
func (s *FeishuPersonalService) PrivateTaskMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(s.WrapPrivateTaskHandler(next.ProcessTask))
}

func (s *FeishuPersonalService) WrapPrivateTaskHandler(
	next func(context.Context, *asynq.Task) error,
) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, task *asynq.Task) error {
		fresh, err := s.privateTaskContext(ctx, task)
		if err != nil {
			return err
		}
		return next(fresh, task)
	}
}

type privateTaskBinding struct {
	TenantID          uint64 `json:"tenant_id"`
	KnowledgeBaseID   string `json:"knowledge_base_id"`
	KBID              string `json:"kb_id"`
	KnowledgeID       string `json:"knowledge_id"`
	ChunkID           string `json:"chunk_id"`
	PersonalAttemptID string `json:"personal_attempt_id"`
}

func (s *FeishuPersonalService) privateTaskContext(ctx context.Context, task *asynq.Task) (context.Context, error) {
	if !types.PrivateKBGuardsEnabled(s.db) {
		return ctx, nil
	}
	switch task.Type() {
	case types.TypeDocumentProcess, types.TypeManualProcess, types.TypeKnowledgePostProcess,
		types.TypeSummaryGeneration, types.TypeQuestionGeneration, types.TypeImageMultimodal,
		types.TypeChunkExtract, types.TypeDataTableSummary, types.TypeKnowledgeAutoTag,
		types.TypeKnowledgeBaseProfile, types.TypeWikiIngest, types.TypeWikiFinalize:
	default:
		return ctx, nil
	}
	var binding privateTaskBinding
	if err := json.Unmarshal(task.Payload(), &binding); err != nil {
		return ctx, errors.Join(err, asynq.SkipRetry)
	}
	if binding.PersonalAttemptID != "" {
		return ctx, nil // PersonalRecovery validates its own durable identity/lease.
	}
	if binding.KnowledgeBaseID == "" {
		binding.KnowledgeBaseID = binding.KBID
	} else if binding.KBID != "" && binding.KBID != binding.KnowledgeBaseID {
		return ctx, errors.Join(access.ErrForbidden, asynq.SkipRetry)
	}
	// Only binding metadata is read before authority reconstruction, never a
	// document body or chunk content. Every supplied parent/tenant must match.
	if binding.ChunkID != "" {
		var row struct{ KnowledgeID, KnowledgeBaseID string }
		if err := s.db.WithContext(ctx).Raw(`SELECT knowledge_id,knowledge_base_id FROM chunks
			WHERE id=? AND tenant_id=?`, binding.ChunkID, binding.TenantID).Scan(&row).Error; err != nil {
			return ctx, err
		}
		if row.KnowledgeID == "" || binding.KnowledgeID != "" && binding.KnowledgeID != row.KnowledgeID ||
			binding.KnowledgeBaseID != "" && binding.KnowledgeBaseID != row.KnowledgeBaseID {
			return ctx, errors.Join(access.ErrForbidden, asynq.SkipRetry)
		}
		binding.KnowledgeID, binding.KnowledgeBaseID = row.KnowledgeID, row.KnowledgeBaseID
	}
	if binding.KnowledgeID != "" {
		var row struct {
			KnowledgeBaseID string
			TenantID        uint64
		}
		if err := s.db.WithContext(ctx).Raw(`SELECT knowledge_base_id,tenant_id FROM knowledges
			WHERE id=? AND deleted_at IS NULL`, binding.KnowledgeID).Scan(&row).Error; err != nil {
			return ctx, err
		}
		if row.KnowledgeBaseID == "" {
			return ctx, nil // Existing handlers own their deleted/missing semantics.
		}
		if row.TenantID != binding.TenantID ||
			binding.KnowledgeBaseID != "" && binding.KnowledgeBaseID != row.KnowledgeBaseID {
			return ctx, errors.Join(access.ErrForbidden, asynq.SkipRetry)
		}
		binding.KnowledgeBaseID = row.KnowledgeBaseID
	}
	owner, err := types.PrivateKBOwner(s.db.WithContext(ctx), binding.KnowledgeBaseID)
	if err != nil || owner == "" {
		return ctx, err
	}
	if user, ok := types.UserIDFromContext(ctx); ok && user != owner {
		return ctx, errors.Join(access.ErrForbidden, asynq.SkipRetry)
	}
	if principal, ok := types.PrincipalFromContext(ctx); ok &&
		(principal.Type != types.PrincipalWebUser || principal.ID != owner) {
		return ctx, errors.Join(access.ErrForbidden, asynq.SkipRetry)
	}
	fresh, err := s.privateTaskOwner(ctx, binding, owner)
	if err != nil {
		return ctx, err
	}
	check := func(call context.Context) error {
		current, err := s.privateTaskOwner(call, binding, owner)
		if err != nil {
			return err
		}
		return s.privateTaskSources(current, binding, owner)
	}
	if err := check(fresh); err != nil {
		return ctx, err
	}
	return types.WithFeishuSourceReadGuard(fresh, check), nil
}

func (s *FeishuPersonalService) privateTaskOwner(
	ctx context.Context, binding privateTaskBinding, owner string,
) (context.Context, error) {
	var user types.User
	var member types.TenantMember
	var tenant types.Tenant
	db := s.db.WithContext(ctx)
	if err := db.Where("id=? AND is_active=?", owner, true).First(&user).Error; err != nil {
		return ctx, err
	}
	if err := db.Where("user_id=? AND tenant_id=? AND status=?", owner, binding.TenantID,
		types.TenantMemberStatusActive).First(&member).Error; err != nil {
		return ctx, err
	}
	if !member.Role.HasPermission(types.TenantRoleContributor) {
		return ctx, errors.Join(access.ErrForbidden, asynq.SkipRetry)
	}
	if err := db.Where("id=? AND status=?", binding.TenantID, "active").First(&tenant).Error; err != nil {
		return ctx, err
	}
	fresh := context.WithValue(ctx, types.KBGrantsContextKey, nil)
	fresh = context.WithValue(fresh, types.UserIDContextKey, owner)
	fresh = context.WithValue(fresh, types.SystemAdminContextKey, false)
	fresh = types.WithCaller(fresh, types.Caller{TenantID: binding.TenantID, UserID: owner, Role: member.Role})
	fresh = types.WithPrincipal(fresh, types.Principal{Type: types.PrincipalWebUser, ID: owner})
	fresh = types.WithExecutionTenant(fresh, binding.TenantID)
	kb, err := s.kbs.GetKnowledgeBaseByID(fresh, binding.KnowledgeBaseID)
	if err != nil {
		return ctx, err
	}
	if kb.TenantID != binding.TenantID || kb.CreatorID != owner || kb.PrivateOwnerID != owner {
		return ctx, errors.Join(access.ErrForbidden, asynq.SkipRetry)
	}
	grant, err := access.ResolveKB(fresh, access.KBRequest{Caller: types.CallerFromContext(fresh)},
		kb, types.OrgRoleEditor, nil, nil)
	if err != nil {
		return ctx, err
	}
	return grant.Context(fresh), nil
}

// Recheck local membership and every relevant Feishu reference at each model
// boundary. KB-wide work is bounded; it cannot silently omit an unverified row.
func (s *FeishuPersonalService) privateTaskSources(
	ctx context.Context, binding privateTaskBinding, owner string,
) error {
	var rows []types.Knowledge
	query := s.db.WithContext(ctx).Select("id", "tenant_id", "knowledge_base_id", "channel", "metadata").
		Where("knowledge_base_id=? AND tenant_id=?", binding.KnowledgeBaseID, binding.TenantID)
	if binding.KnowledgeID != "" {
		query = query.Where("id=?", binding.KnowledgeID)
	}
	if err := query.Limit(1001).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) > 1000 || binding.KnowledgeID != "" && len(rows) != 1 {
		return errors.Join(access.ErrForbidden, asynq.SkipRetry)
	}
	for _, row := range rows {
		var metadata map[string]any
		if len(row.Metadata) > 0 && json.Unmarshal(row.Metadata, &metadata) != nil {
			return repository.ErrPersonalIdentity
		}
		attempt, _ := metadata["feishu_mvp_attempt"].(string)
		if attempt == "" {
			if row.Channel == "feishu_personal" {
				return repository.ErrPersonalIdentity
			}
			continue
		}
		if s.recovery == nil {
			return repository.ErrPersonalIdentity
		}
		m, resource, a, err := s.recovery.repo.Load(ctx, attempt)
		if err != nil {
			return err
		}
		if m.UserID != owner || m.KBID != binding.KnowledgeBaseID || m.KnowledgeID != row.ID ||
			resource.CurrentAttemptID != a.ID || resource.Generation != a.Fence ||
			m.Status == "source_managed" || m.Status == "paused" || m.Status == "hold" || m.Status == "revoked" ||
			a.Status == "obsolete" || a.Status == "needs_attention" {
			return repository.ErrPersonalIdentity
		}
		fresh, err := s.recovery.authorize(ctx, m, a)
		if err != nil {
			return err
		}
		cfg, err := s.app(fresh)
		if err != nil {
			return err
		}
		link, err := core.ParsePersonalResourceURL(m.URL)
		if err != nil {
			return err
		}
		resolved, err := s.client(&core.Config{AppID: cfg.AppID, AppSecret: cfg.Secret}).
			ResolveBoundResource(fresh, link)
		if err != nil {
			return err
		}
		if resolved.Type != m.ResourceType || resolved.Token != m.DocumentToken ||
			m.SourceModified != "" && resolved.Modified != m.SourceModified {
			return repository.ErrPersonalIdentity
		}
	}
	return nil
}
