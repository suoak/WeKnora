package repository

import (
	"context"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/types"
)

type sourceReparseDeliveryKey struct{}

type sourceReparseDeliverySnapshot struct {
	request types.FeishuDocumentReparse
}

// HasSourceDocumentReparseDelivery identifies a server-installed continuation,
// never an HTTP principal. CheckSourceDocumentReparseDelivery must still reload
// the exact accepted SQL intent before scheduler admission.
func HasSourceDocumentReparseDelivery(ctx context.Context) bool {
	_, ok := ctx.Value(sourceReparseDeliveryKey{}).(sourceReparseDeliverySnapshot)
	return ok
}

// PrepareSourceDocumentReparseDelivery binds a background wake-up to an existing
// accepted intent. It grants neither user credentials nor outbound permission.
func (s *FeishuLegacyStore) PrepareSourceDocumentReparseDelivery(
	ctx context.Context, ds *types.DataSource, request *types.FeishuDocumentReparse,
) (context.Context, error) {
	if err := s.checkSourceReparseDeliverySnapshot(ctx, ds, request); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, sourceReparseDeliveryKey{}, sourceReparseDeliverySnapshot{request: *request}), nil
}

// CheckSourceDocumentReparseDelivery rechecks the original SQL snapshot and
// audit actor immediately before the existing scheduler admission gate.
func (s *FeishuLegacyStore) CheckSourceDocumentReparseDelivery(
	ctx context.Context, ds *types.DataSource,
) (*types.FeishuDocumentReparse, error) {
	snapshot, ok := ctx.Value(sourceReparseDeliveryKey{}).(sourceReparseDeliverySnapshot)
	if !ok {
		return nil, legacyFence("document_delivery_intent_missing")
	}
	if err := s.checkSourceReparseDeliverySnapshot(ctx, ds, &snapshot.request); err != nil {
		return nil, err
	}
	return &snapshot.request, nil
}

func (s *FeishuLegacyStore) checkSourceReparseDeliverySnapshot(
	ctx context.Context, ds *types.DataSource, request *types.FeishuDocumentReparse,
) error {
	// Restored task audit data must never become a WebUser/API-key session.
	if ctx.Value(types.PrincipalContextKey) != nil {
		return legacyFence("document_delivery_http_identity")
	}
	if _, keyed := types.TenantAPIKeyScopeFromContext(ctx); keyed {
		return legacyFence("document_delivery_http_identity")
	}
	actor, _ := types.UserIDFromContext(ctx)
	tenant, _ := types.TenantIDFromContext(ctx)
	if ds == nil || request == nil || request.ID == "" || request.KnowledgeID == "" ||
		actor == "" || types.IsSyntheticUserID(actor) || actor != request.Initiator.UserID || tenant != ds.TenantID ||
		request.DataSourceID != ds.ID || request.TenantID != ds.TenantID ||
		request.KnowledgeBaseID != ds.KnowledgeBaseID || request.ConfigHash != LegacySourceReparseConfigHash(ds) {
		return legacyFence("document_delivery_binding_changed")
	}
	expected, err := json.Marshal(request)
	if err != nil {
		return err
	}
	var count int64
	err = s.db.WithContext(ctx).Model(&types.TaskPendingOp{}).
		Where("task_type=? AND scope=? AND scope_id=? AND tenant_id=? AND dedup_key=?",
			sourceDocumentReparseTask, sourceDocumentReparseScope, ds.ID, ds.TenantID, request.KnowledgeID).
		Where("op IN ? AND payload=?::jsonb", []string{"queued", "processing"}, string(expected)).
		Where("COALESCE(NULLIF(payload->>'retry_after',''),'0001-01-01')::timestamptz <= clock_timestamp()").
		Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return legacyFence("document_delivery_intent_changed")
	}
	return nil
}
