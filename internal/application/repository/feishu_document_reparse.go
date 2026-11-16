package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	sourceDocumentReparseScope = "feishu_document_reparse_v1"
	sourceDocumentReparseTask  = "feishu:document:reparse"
)

// LegacySourceReparseConfigHash binds a request to the complete persisted source selection and identity.
func LegacySourceReparseConfigHash(ds *types.DataSource) string {
	data, _ := json.Marshal(
		[]any{ds.ID, ds.TenantID, ds.KnowledgeBaseID, ds.Type, ds.LegacyAccessState, ds.Config},
	)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// SourceReparseTarget projects only connector identity, including held historical metadata.
func (s *FeishuLegacyStore) SourceReparseTarget(
	ctx context.Context, ds *types.DataSource, kid string,
) (*types.FeishuDocumentReparse, error) {
	var target struct {
		ExternalID, RootID, FileType string
		ProcessConfig                json.RawMessage
	}
	err := s.db.WithContext(ctx).Table("knowledges k").
		Select("COALESCE(NULLIF(r.external_id,''),k.metadata->>'external_id') AS external_id,"+
			"COALESCE(NULLIF(k.metadata->>'source_resource_id',''),"+
			"r.quarantine_snapshot->'knowledge'->'metadata'->>'source_resource_id') AS root_id,"+
			"COALESCE(NULLIF(k.file_type,''),r.quarantine_snapshot->'knowledge'->>'file_type') AS file_type,"+
			"COALESCE(k.metadata->'process_overrides',"+
			"r.quarantine_snapshot->'knowledge'->'metadata'->'process_overrides','null'::jsonb) AS process_config").
		Joins("LEFT JOIN feishu_legacy_resources r ON r.knowledge_id=k.id AND r.tenant_id=k.tenant_id"+
			" AND r.knowledge_base_id=k.knowledge_base_id AND r.data_source_id=?", ds.ID).
		Where("k.id=? AND k.tenant_id=? AND k.knowledge_base_id=? AND k.deleted_at IS NULL", kid,
			ds.TenantID, ds.KnowledgeBaseID).
		Where("r.id IS NOT NULL OR (k.channel=? AND k.metadata->>'datasource_id'=?)", ds.Type, ds.ID).
		Scan(&target).Error
	if err != nil {
		return nil, err
	}
	if target.RootID == "" {
		config, err := ds.ParseConfig()
		if err == nil && config != nil && len(config.ResourceIDs) == 1 {
			// Historical rows may predate root metadata. The selected root is
			// still re-listed to prove document membership before any body read.
			target.RootID = config.ResourceIDs[0]
		}
	}
	if target.RootID == "" || target.ExternalID == "" || target.FileType == "" {
		return nil, legacyFence("document_binding_unavailable")
	}
	var processConfig any
	if err := json.Unmarshal(target.ProcessConfig, &processConfig); err != nil {
		return nil, err
	}
	return &types.FeishuDocumentReparse{
		KnowledgeID: kid, DataSourceID: ds.ID,
		KnowledgeBaseID: ds.KnowledgeBaseID, TenantID: ds.TenantID,
		RootID: target.RootID, ExternalID: target.ExternalID, FileType: target.FileType,
		ConfigHash:          LegacySourceReparseConfigHash(ds),
		PreviousProcessHash: sourceProcessConfigHash(processConfig),
	}, nil
}

func reparseStatus(
	op *types.TaskPendingOp,
	request *types.FeishuDocumentReparse,
) *types.FeishuDocumentReparseStatus {
	return &types.FeishuDocumentReparseStatus{
		ID: request.ID, KnowledgeID: request.KnowledgeID,
		State: op.Op, Attempts: request.Attempts, ErrorCode: request.ErrorCode,
		CompletedChunks: request.CompletedChunks, TotalChunks: request.TotalChunks,
	}
}

// QueueSourceDocumentReparse serializes duplicates without modifying any public document or sync cursor.
func (s *FeishuLegacyStore) QueueSourceDocumentReparse(
	ctx context.Context, ds *types.DataSource, request *types.FeishuDocumentReparse,
) (*types.FeishuDocumentReparseStatus, error) {
	if request == nil || request.KnowledgeID == "" || request.ExternalID == "" ||
		request.RootID == "" ||
		request.Initiator.UserID == "" ||
		types.IsSyntheticUserID(request.Initiator.UserID) {
		return nil, legacyFence("document_request_invalid")
	}
	var result *types.FeishuDocumentReparseStatus
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Name() != "postgres" {
			return legacyFence("repository_not_supported")
		}
		current, err := lockSyncDataSource(tx, ds.ID, ds.TenantID)
		if err != nil {
			return err
		}
		if request.ConfigHash != LegacySourceReparseConfigHash(current) ||
			request.DataSourceID != ds.ID ||
			request.TenantID != ds.TenantID ||
			request.KnowledgeBaseID != ds.KnowledgeBaseID {
			return legacyFence("source_configuration_changed")
		}
		target, err := NewFeishuLegacyStore(
			tx,
		).SourceReparseTarget(ctx, current, request.KnowledgeID)
		if err != nil {
			return err
		}
		if target.RootID != request.RootID || target.ExternalID != request.ExternalID ||
			target.FileType != request.FileType || target.PreviousProcessHash != request.PreviousProcessHash {
			return legacyFence("document_binding_changed")
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))",
			"feishu-document-reparse:"+request.KnowledgeID).Error; err != nil {
			return err
		}
		var existing []types.TaskPendingOp
		if err := tx.Where("task_type=? AND scope=? AND scope_id=? AND dedup_key=? AND op IN ?",
			sourceDocumentReparseTask, sourceDocumentReparseScope, ds.ID, request.KnowledgeID,
			[]string{"queued", "processing"}).Order("id").Limit(2).Find(&existing).Error; err != nil {
			return err
		}
		if len(existing) > 0 {
			var accepted types.FeishuDocumentReparse
			if len(existing) != 1 || json.Unmarshal(existing[0].Payload, &accepted) != nil {
				return legacyFence("document_request_ambiguous")
			}
			a, _ := json.Marshal(request.ProcessConfig)
			b, _ := json.Marshal(accepted.ProcessConfig)
			if accepted.ConfigHash != request.ConfigHash ||
				accepted.KBConfigHash != request.KBConfigHash ||
				accepted.RootID != request.RootID ||
				accepted.ExternalID != request.ExternalID ||
				accepted.FileType != request.FileType ||
				accepted.PreviousProcessHash != request.PreviousProcessHash ||
				accepted.Initiator.UserID != request.Initiator.UserID ||
				!bytes.Equal(a, b) {
				return ErrLegacyVersionBusy
			}
			result = reparseStatus(&existing[0], &accepted)
			return nil
		}
		var count int64
		if err := tx.Model(&types.TaskPendingOp{}).Where("scope=? AND scope_id=? AND op IN ?",
			sourceDocumentReparseScope, ds.ID, []string{"queued", "processing"}).Count(&count).Error; err != nil {
			return err
		}
		if count >= 200 {
			return ErrLegacyVersionBusy
		}
		request.ID, request.RequestedAt = uuid.NewString(), time.Now().UTC()
		payload, err := json.Marshal(request)
		if err != nil {
			return err
		}
		if len(payload) > 256<<10 {
			return legacyFence("document_request_too_large")
		}
		op := &types.TaskPendingOp{
			TenantID: ds.TenantID, TaskType: sourceDocumentReparseTask,
			Scope: sourceDocumentReparseScope, ScopeID: ds.ID, Op: "queued", DedupKey: request.KnowledgeID,
			Payload: payload, EnqueuedAt: request.RequestedAt,
		}
		if err := tx.Create(op).Error; err != nil {
			return err
		}
		result = reparseStatus(op, request)
		return nil
	})
	return result, err
}

type (
	sourceDocumentReparseKey   struct{}
	sourceDocumentReparseClaim struct {
		op      types.TaskPendingOp
		request types.FeishuDocumentReparse
	}
)

// ClaimSourceDocumentReparse requires the normal datasource execution fence.
// Old processing requests can only be reclaimed by a newly valid source execution.
func (s *FeishuLegacyStore) ClaimSourceDocumentReparse(
	ctx context.Context, ds *types.DataSource,
) (context.Context, *types.FeishuDocumentReparse, error) {
	claim := &sourceDocumentReparseClaim{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.execution(ctx, tx, ds); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"task_type=? AND scope=? AND scope_id=? AND tenant_id=? AND op IN ?",
			sourceDocumentReparseTask, sourceDocumentReparseScope, ds.ID, ds.TenantID,
			[]string{"queued", "processing"}).
			Where("COALESCE(NULLIF(payload->>'retry_after',''),'0001-01-01')::timestamptz <= clock_timestamp()").
			Order("id").First(&claim.op).Error; err != nil {
			return err
		}
		if err := json.Unmarshal(claim.op.Payload, &claim.request); err != nil {
			return err
		}
		request := &claim.request
		if request.DataSourceID != ds.ID || request.TenantID != ds.TenantID ||
			request.KnowledgeBaseID != ds.KnowledgeBaseID || request.KnowledgeID != claim.op.DedupKey ||
			request.ID == "" || request.RootID == "" || request.ExternalID == "" {
			return legacyFence("document_request_invalid")
		}
		if request.RetryAfter.After(time.Now()) {
			return gorm.ErrRecordNotFound
		}
		owner, _ := types.SyncExecutionFromContext(ctx)
		if claim.op.Op == "processing" && request.ExecutionID == owner.SyncLogID &&
			request.Generation == owner.Generation {
			return gorm.ErrRecordNotFound
		}
		request.ExecutionID, request.Generation = owner.SyncLogID, owner.Generation
		request.Attempts++
		if request.ConfigHash != LegacySourceReparseConfigHash(ds) || request.Attempts > 3 {
			request.ErrorCode = "source_configuration_changed"
			if request.Attempts > 3 {
				request.ErrorCode = "retry_exhausted"
			}
			claim.op.Op = "failed"
		} else {
			claim.op.Op = "processing"
		}
		payload, err := json.Marshal(request)
		if err != nil {
			return err
		}
		claim.op.Payload = payload
		return tx.Save(&claim.op).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ctx, nil, nil
	}
	if err != nil {
		return ctx, nil, err
	}
	if claim.op.Op != "processing" {
		return ctx, nil, nil
	}
	return context.WithValue(ctx, sourceDocumentReparseKey{}, claim), &claim.request, nil
}

func sourceProcessConfigHash(value any) string {
	data, _ := json.Marshal(value)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// An explicit configuration change is permitted only by the immutable claimed
// request. The previous configuration remains fenced against concurrent edits.
func sourceReparseProcessMetadata(
	ctx context.Context, tx *gorm.DB, ds *types.DataSource, r *types.FeishuLegacyResource,
	previous, candidate *types.Knowledge, normalize bool,
) error {
	claim, err := checkSourceReparseClaim(ctx, tx, ds, r)
	if err != nil {
		return err
	}
	if claim == nil {
		return legacyProcessMetadata(previous, candidate, normalize)
	}
	old, err := previous.Metadata.Map()
	if err != nil {
		return err
	}
	if r.State == "hold" && old["process_overrides"] == nil && len(r.QuarantineSnapshot) > 2 {
		var snapshot struct {
			Knowledge types.Knowledge `json:"knowledge"`
		}
		if err := json.Unmarshal(r.QuarantineSnapshot, &snapshot); err != nil {
			return err
		}
		private := &snapshot.Knowledge
		if private.ID != previous.ID || private.TenantID != previous.TenantID ||
			private.KnowledgeBaseID != previous.KnowledgeBaseID {
			return legacyFence("document_snapshot_binding_changed")
		}
		previous = private
		old, err = previous.Metadata.Map()
		if err != nil {
			return err
		}
	}
	if sourceProcessConfigHash(old["process_overrides"]) != claim.request.PreviousProcessHash {
		return legacyFence("knowledge_process_config_changed")
	}
	if claim.request.ProcessConfig == nil {
		return legacyProcessMetadata(previous, candidate, normalize)
	}
	actual, err := candidate.ProcessOverrides()
	if err != nil {
		return err
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(claim.request.ProcessConfig)
	if !bytes.Equal(a, b) {
		return legacyFence("knowledge_process_config_changed")
	}
	return nil
}

func checkSourceReparseClaim(
	ctx context.Context, tx *gorm.DB, ds *types.DataSource, r *types.FeishuLegacyResource,
) (*sourceDocumentReparseClaim, error) {
	claim, present := ctx.Value(sourceDocumentReparseKey{}).(*sourceDocumentReparseClaim)
	if !present {
		return nil, nil
	}
	owner, ok := types.SyncExecutionFromContext(ctx)
	if claim == nil || !ok || claim.request.ExecutionID != owner.SyncLogID ||
		claim.request.Generation != owner.Generation || claim.request.DataSourceID != ds.ID ||
		claim.request.KnowledgeID != r.KnowledgeID || claim.request.ExternalID != r.ExternalID ||
		claim.request.ConfigHash != LegacySourceReparseConfigHash(ds) {
		return nil, legacyFence("document_request_binding_changed")
	}
	var live types.TaskPendingOp
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND op='processing'",
		claim.op.ID).First(&live).Error; err != nil {
		return nil, err
	}
	var current types.FeishuDocumentReparse
	if json.Unmarshal(live.Payload, &current) != nil {
		return nil, legacyFence("document_request_invalid")
	}
	a, _ := json.Marshal(current)
	b, _ := json.Marshal(claim.request)
	if !bytes.Equal(a, b) {
		return nil, legacyFence("document_request_owner_changed")
	}
	return claim, nil
}

// SourceDocumentReparseOverrides is available only inside a DB-issued source request claim.
func SourceDocumentReparseOverrides(ctx context.Context) (*types.KnowledgeProcessOverrides, bool) {
	claim, ok := ctx.Value(sourceDocumentReparseKey{}).(*sourceDocumentReparseClaim)
	if !ok || claim == nil {
		return nil, false
	}
	if claim.request.ProcessConfig == nil {
		return nil, true
	}
	var config types.KnowledgeProcessOverrides
	data, _ := json.Marshal(claim.request.ProcessConfig)
	_ = json.Unmarshal(data, &config)
	return &config, true
}

func completeSourceDocumentReparse(
	ctx context.Context,
	tx *gorm.DB,
	ds *types.DataSource,
	r *types.FeishuLegacyResource,
	attempt string,
) error {
	claim, err := checkSourceReparseClaim(ctx, tx, ds, r)
	if err != nil || claim == nil {
		return err
	}
	request := claim.request
	request.Published, request.ErrorCode = attempt, ""
	request.CompletedChunks = request.TotalChunks
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	updated := tx.Model(&types.TaskPendingOp{}).Where("id=? AND op='processing'", claim.op.ID).
		Updates(map[string]any{"op": "completed", "payload": json.RawMessage(payload)})
	if updated.Error == nil && updated.RowsAffected != 1 {
		return legacyFence("document_request_completion_lost")
	}
	return updated.Error
}

// SourceDocumentReparseStatus loads only the most recent request for one document.
func (s *FeishuLegacyStore) SourceDocumentReparseStatus(
	ctx context.Context, ds *types.DataSource, kid string,
) (*types.FeishuDocumentReparseStatus, error) {
	var op types.TaskPendingOp
	if err := s.db.WithContext(ctx).Where("task_type=? AND scope=? AND scope_id=? AND tenant_id=? AND dedup_key=?",
		sourceDocumentReparseTask, sourceDocumentReparseScope, ds.ID, ds.TenantID, kid).
		Order("id DESC").First(&op).Error; err != nil {
		return nil, err
	}
	var request types.FeishuDocumentReparse
	if err := json.Unmarshal(op.Payload, &request); err != nil {
		return nil, err
	}
	return reparseStatus(&op, &request), nil
}

// SourceDocumentReparseStatuses projects request progress for already authorized catalog rows.
func (s *FeishuLegacyStore) SourceDocumentReparseStatuses(
	ctx context.Context, tenant uint64, ids []string,
) ([]types.FeishuDocumentReparseStatus, error) {
	if len(ids) == 0 || len(ids) > 2000 {
		return nil, nil
	}
	var results []types.FeishuDocumentReparseStatus
	err := s.db.WithContext(ctx).Table("task_pending_ops").
		Select("DISTINCT ON (dedup_key) payload->>'id' AS id,dedup_key AS knowledge_id,op AS state,"+
			"COALESCE((payload->>'attempts')::int,0) AS attempts,COALESCE(payload->>'error_code','') AS error_code,"+
			"COALESCE((payload->>'completed_chunks')::int,0) AS completed_chunks,"+
			"COALESCE((payload->>'total_chunks')::int,0) AS total_chunks").
		Where("task_type=? AND scope=? AND tenant_id=? AND dedup_key IN ?", sourceDocumentReparseTask,
			sourceDocumentReparseScope, tenant, ids).
		Order("dedup_key,task_pending_ops.id DESC").Scan(&results).Error
	return results, err
}

// FinishSourceDocumentReparse records bounded retries without changing public knowledge.
// Successful completion is written only inside the publication transaction.
func (s *FeishuLegacyStore) FinishSourceDocumentReparse(
	ctx context.Context, ds *types.DataSource, code string, retry bool, busy bool,
) error {
	claim, ok := ctx.Value(sourceDocumentReparseKey{}).(*sourceDocumentReparseClaim)
	if !ok || claim == nil {
		return legacyFence("document_request_missing")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.executionFence(ctx, tx, ds); err != nil {
			return err
		}
		resource := &types.FeishuLegacyResource{
			KnowledgeID: claim.request.KnowledgeID,
			ExternalID:  claim.request.ExternalID,
		}
		if _, err := checkSourceReparseClaim(ctx, tx, ds, resource); err != nil {
			return err
		}
		request := claim.request
		request.ErrorCode = code
		state := "failed"
		if busy || (retry && request.Attempts < 3) {
			state = "queued"
			delay := time.Minute << uint(min(request.Attempts-1, 2))
			if busy {
				request.Attempts--
				delay = 20 * time.Minute
			}
			request.RetryAfter = time.Now().UTC().Add(delay)
		}
		payload, err := json.Marshal(request)
		if err != nil {
			return err
		}
		return tx.Model(&types.TaskPendingOp{}).Where("id=?", claim.op.ID).
			Updates(map[string]any{"op": state, "payload": json.RawMessage(payload)}).Error
	})
}

// PendingSourceDocumentReparses returns bounded wake-up identities, never body content or credentials.
func (s *FeishuLegacyStore) PendingSourceDocumentReparses(
	ctx context.Context,
) ([]types.FeishuDocumentReparse, error) {
	var ops []types.TaskPendingOp
	if err := s.db.WithContext(ctx).Where("task_type=? AND scope=? AND op IN ?",
		sourceDocumentReparseTask, sourceDocumentReparseScope, []string{"queued", "processing"}).
		Where("COALESCE(NULLIF(payload->>'retry_after',''),'0001-01-01')::timestamptz <= clock_timestamp()").
		Where("id IN (SELECT MIN(id) FROM task_pending_ops WHERE task_type=? AND scope=? AND op IN ?"+
			" AND COALESCE(NULLIF(payload->>'retry_after',''),'0001-01-01')::timestamptz <= clock_timestamp()"+
			" GROUP BY scope_id ORDER BY MIN(id) LIMIT 32)", sourceDocumentReparseTask,
			sourceDocumentReparseScope, []string{"queued", "processing"}).
		Order("id").Find(&ops).Error; err != nil {
		return nil, err
	}
	requests := make([]types.FeishuDocumentReparse, 0, len(ops))
	for _, op := range ops {
		var request types.FeishuDocumentReparse
		if err := json.Unmarshal(op.Payload, &request); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

// RejectSourceDocumentReparse closes an undeliverable intent without reviving a source or cancelling a live worker.
func (s *FeishuLegacyStore) RejectSourceDocumentReparse(
	ctx context.Context, request *types.FeishuDocumentReparse, code string,
) error {
	expected, err := json.Marshal(request)
	if err != nil {
		return err
	}
	snapshot := *request
	snapshot.ErrorCode = code
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&types.TaskPendingOp{}).
		Where("scope=? AND scope_id=? AND tenant_id=? AND dedup_key=? AND payload=?::jsonb",
			sourceDocumentReparseScope, request.DataSourceID, request.TenantID, request.KnowledgeID,
			string(expected)).
		Where("op='queued' OR (op='processing' AND NOT EXISTS (SELECT 1 FROM sync_logs l"+
			" WHERE l.data_source_id=? AND l.status='running' AND l.execution_claimed))", request.DataSourceID).
		Updates(map[string]any{"op": "failed", "payload": json.RawMessage(payload)}).Error
}

// DeferSourceDocumentReparse backs off delivery failures independently of actual parser attempts.
func (s *FeishuLegacyStore) DeferSourceDocumentReparse(
	ctx context.Context, request *types.FeishuDocumentReparse,
) error {
	expected, err := json.Marshal(request)
	if err != nil {
		return err
	}
	snapshot := *request
	snapshot.DeliveryAttempts = min(snapshot.DeliveryAttempts+1, 64)
	snapshot.RetryAfter = time.Now().
		UTC().
		Add(time.Minute << uint(min(snapshot.DeliveryAttempts-1, 4)))
	snapshot.ErrorCode = "queue_delivery_pending"
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&types.TaskPendingOp{}).
		Where("scope=? AND scope_id=? AND tenant_id=? AND dedup_key=? AND op='queued' AND payload=?::jsonb",
			sourceDocumentReparseScope, request.DataSourceID, request.TenantID, request.KnowledgeID,
			string(expected)).
		Updates(map[string]any{"payload": json.RawMessage(payload)}).Error
}
