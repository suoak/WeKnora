package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func (p *PersonalRecovery) prepareExactPersonalResume(
	ctx context.Context, observed *types.FeishuPersonalAttempt,
) error {
	if observed == nil || !p.firstCreateOnly || !exactPersonalRecoveryEnabled() ||
		(os.Getenv("FEISHU_PERSONAL_EXACT_RECOVERY_AUTOMATIC") != "1" &&
			!exactPersonalRecoveryAllows(observed.LogicalKnowledgeID)) ||
		observed.AuthMode != types.FeishuUserOAuth ||
		observed.Status != "needs_attention" || observed.RetryCount >= 3 ||
		observed.LastErrorCategory != "exact_document_execution_requires_review" ||
		!strings.HasSuffix(strings.ToLower(observed.ManifestName), ".docx") {
		return nil
	}
	preflight := &personalExactPreflight{observed: observed, closed: true}
	if _, err := p.checkExactPersonalSource(ctx, observed, false, preflight); err != nil {
		return err
	}
	if preflight.source == nil || preflight.validate == nil {
		return errPersonalExactDocument
	}
	if err := p.repo.QueueExactResume(ctx, observed, exactSourceReceipt(preflight.source),
		preflight.validate); err != nil {
		if errors.Is(err, repository.ErrPersonalBusy) {
			return nil
		}
		return err
	}
	ks := p.service.knowledge.(*knowledgeService)
	dispatch := ks.repo.(interfaces.KnowledgeDispatchRepository)
	op, err := dispatch.GetKnowledgeDispatch(ctx, observed.CallerTenantID,
		observed.LogicalKnowledgeID)
	if err != nil {
		return err
	}
	return ks.publishKnowledgeDispatch(ctx, op)
}

// This is a server-owned durable state, not a task/header opt-in. It cannot
// open a body; the claimed consumer still rederives and compares every chunk.
func (p *PersonalRecovery) exactResumeQueued(ctx context.Context, knowledge string) (bool, error) {
	var rows []types.TaskPendingOp
	if err := p.service.db.WithContext(ctx).Where("scope=? AND scope_id=?",
		types.PersonalExactReceiptScope, knowledge).Limit(2).Find(&rows).Error; err != nil {
		return false, err
	}
	if len(rows) == 0 || (len(rows) == 1 && rows[0].Op != "resuming") {
		return false, nil
	}
	var source types.PersonalExactReceipt
	if len(rows) != 1 || json.Unmarshal(rows[0].Payload, &source) != nil ||
		source.KnowledgeID != knowledge || source.Version != 1 {
		return false, repository.ErrPersonalIdentity
	}
	return true, nil
}
