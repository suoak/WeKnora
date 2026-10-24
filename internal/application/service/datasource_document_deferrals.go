package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ReplaceDocumentDeferrals records an authenticated administrator's exact-document policy.
func (s *DataSourceService) ReplaceDocumentDeferrals(
	ctx context.Context, id, expected string, entries []types.SyncDocumentDeferral,
) (*types.DataSource, error) {
	caller := types.CallerFromContext(ctx)
	tenant := caller.TenantID
	if tenant == 0 {
		return nil, errors.New("workspace identity required")
	}
	actor := caller.UserID
	systemAdmin, _ := ctx.Value(types.SystemAdminContextKey).(bool)
	if actor == "" || (!systemAdmin && caller.Role != types.TenantRoleOwner && caller.Role != types.TenantRoleAdmin) {
		return nil, errors.New("authenticated policy author required")
	}
	source, err := s.dsRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if source.TenantID != tenant || !legacyWikiDrive(source.Type) {
		return nil, errors.New("document policy source scope mismatch")
	}
	old, err := source.ParseDocumentDeferrals()
	if err != nil {
		return nil, err
	}
	prior := map[string]types.SyncDocumentDeferral{}
	for _, v := range old {
		prior[v.Key()] = v
	}
	for i := range entries {
		v := &entries[i]
		if (v.TenantID != 0 && v.TenantID != source.TenantID) ||
			(v.DataSourceID != "" && v.DataSourceID != source.ID) ||
			(v.KnowledgeBaseID != "" && v.KnowledgeBaseID != source.KnowledgeBaseID) {
			return nil, errors.New("cross-source document deferral rejected")
		}
		v.TenantID, v.DataSourceID, v.KnowledgeBaseID = source.TenantID, source.ID, source.KnowledgeBaseID
		previous, exists := prior[v.Key()]
		if exists && previous.ReasonCode == v.ReasonCode && previous.Reason == v.Reason &&
			previous.Reference == v.Reference && previous.Title == v.Title {
			v.RequestedBy, v.RequestedAt = previous.RequestedBy, previous.RequestedAt
		} else {
			v.RequestedBy, v.RequestedAt = actor, time.Now().UTC()
		}
	}
	repo, ok := s.dsRepo.(interfaces.DataSourceDocumentDeferralRepository)
	if !ok {
		return nil, errors.New("document policy repository unavailable")
	}
	if err = repo.ReplaceDocumentDeferrals(ctx, source, expected, entries); err != nil {
		return nil, err
	}
	current, err := source.ParseDocumentDeferrals()
	if err != nil {
		return nil, err
	}
	recordKBActivity(ctx, s.audit, source.TenantID, source.KnowledgeBaseID, types.AuditActionDataSourceUpdated,
		"data_source",
		source.ID,
		types.AuditOutcomeSuccess,
		map[string]any{
			"changed_fields":       []string{"document_deferrals"},
			"previous_policy_hash": expected,
			"policy_hash":          types.DocumentDeferralHash(current),
			"document_count":       len(current),
		})
	return source, nil
}

func (h *streamSyncHandler) DocumentDeferrals(ctx context.Context) ([]types.SyncDocumentDeferral, error) {
	if err := h.svc.checkSyncExecution(ctx); err != nil {
		return nil, err
	}
	if !legacyWikiDrive(h.ds.Type) {
		return nil, nil
	}
	policy, err := h.ds.ParseDocumentDeferrals()
	if err != nil {
		return nil, err
	}
	h.result.Deferred = len(policy)
	h.result.DeferredDocuments = policy
	return policy, nil
}

func (h *streamSyncHandler) ObserveDocument(_ string, externalID, disposition string) {
	if h.observations == nil {
		h.observations = map[string]bool{}
	}
	key := externalID + "\x00" + disposition
	if h.observations[key] {
		return
	}
	h.observations[key] = true
	switch disposition {
	case "discovered":
		h.result.Discovered++
	case "unchanged":
		h.result.Unchanged++
	case "unsupported":
		h.result.Unsupported++
	}
}

func streamingSyncOutcome(result *types.SyncResult) (string, string) {
	status, message := types.SyncLogStatusSuccess, ""
	if result.Failed > 0 || result.Unsupported > 0 || result.Deferred > 0 {
		status = types.SyncLogStatusPartial
		message = fmt.Sprintf("%d document(s) failed to sync", result.Failed)
		if result.Deferred > 0 || result.Unsupported > 0 {
			message += fmt.Sprintf("; %d deferred; %d unsupported", result.Deferred, result.Unsupported)
		}
	}
	if result.Deferred > 0 &&
		result.Created+result.Updated+result.Deleted+result.Skipped+result.Unchanged == 0 &&
		result.Failed == 0 &&
		result.Unsupported == 0 {
		status = types.SyncLogStatusDeferred
	}
	if result.DeletionFailed > 0 {
		message += fmt.Sprintf("; %d deletion failure(s) will only retry on the next full sync", result.DeletionFailed)
	}
	return status, message
}
