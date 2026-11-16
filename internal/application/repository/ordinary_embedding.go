package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrdinaryEmbeddingWindow bounds the persisted work owned by one consumer.
const OrdinaryEmbeddingWindow = 100

var (
	// ErrOrdinaryEmbeddingChanged rejects a stale identity, input or lease.
	ErrOrdinaryEmbeddingChanged = errors.New("ordinary embedding inputs changed")
	// ErrOrdinaryEmbeddingBusy identifies a currently owned live lease.
	ErrOrdinaryEmbeddingBusy = errors.New("ordinary embedding lease busy")
)

// OrdinaryEmbeddingStore is deliberately limited to the default local PG
// index. Managed sources, private KBs and external vector stores retain their
// existing protocols. The outbox is server-owned and survives process loss.
type OrdinaryEmbeddingStore struct{ db *gorm.DB }

func (r *knowledgeRepository) OrdinaryEmbeddingStore() *OrdinaryEmbeddingStore {
	if r.db.Name() != "postgres" {
		return nil
	}
	return &OrdinaryEmbeddingStore{db: r.db}
}

func ordinaryDigest(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	// PostgreSQL JSON/JSONB may reorder nested RawMessages. Canonicalize
	// objects while preserving integer precision before comparing stamps.
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	var normalized any
	if decoder.Decode(&normalized) != nil {
		return ""
	}
	b, err = json.Marshal(normalized)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// OrdinaryEmbeddingChunkStamp binds all persisted text and source coordinates.
func OrdinaryEmbeddingChunkStamp(c *types.Chunk) string {
	return ordinaryDigest(
		[]any{
			c.ID,
			c.TenantID,
			c.KnowledgeID,
			c.KnowledgeBaseID,
			c.Content,
			c.ContextHeader,
			c.ContentRevision,
			c.ChunkIndex,
			c.StartAt,
			c.EndAt,
			c.ParentChunkID,
			c.Metadata,
			c.ImageInfo,
			c.IsEnabled,
			c.ChunkType,
		},
	)
}

// OrdinaryEmbeddingKBStamp hashes normalized processing configuration.
func OrdinaryEmbeddingKBStamp(kb *types.KnowledgeBase) string {
	if kb == nil {
		return ""
	}
	// Copy without invoking the API's redacting MarshalJSON, then normalize
	// exactly as the KB service does. Avoid mutating shared config pointers.
	type rawKB types.KnowledgeBase
	data, err := json.Marshal((*rawKB)(kb))
	if err != nil {
		return ""
	}
	var normalized types.KnowledgeBase
	if json.Unmarshal(data, &normalized) != nil {
		return ""
	}
	normalized.EnsureDefaults()
	return ordinaryDigest([]any{
		normalized.ID, normalized.TenantID, normalized.Type, normalized.CreatorID, normalized.PrivateOwnerID,
		normalized.ChunkingConfig, normalized.ImageProcessingConfig, normalized.EmbeddingModelID,
		normalized.SummaryModelID, normalized.VLMConfig, normalized.ASRConfig, normalized.StorageProviderConfig,
		normalized.StorageBackendID, normalized.VectorStoreID, normalized.ExtractConfig,
		normalized.FAQConfig, normalized.QuestionGenerationConfig, normalized.AutoTagConfig, normalized.ProfileConfig,
		normalized.WikiConfig, normalized.IndexingStrategy,
	})
}

func ordinarySourceStamp(k *types.Knowledge) string {
	overrides, _ := k.ProcessOverrides()
	return ordinaryDigest(
		[]any{
			k.ID,
			k.TenantID,
			k.KnowledgeBaseID,
			k.Channel,
			k.Type,
			k.FileHash,
			k.FilePath,
			k.Title,
			k.CustomMetadata,
			overrides,
		},
	)
}

func ordinaryConfigStamp(tx *gorm.DB, kb *types.KnowledgeBase, exclusive ...bool) (string, error) {
	var model types.Model
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id=? AND status=?", kb.EmbeddingModelID, "active").
		First(&model).Error; err != nil {
		return "", err
	}
	var tenant types.Tenant
	strength := "SHARE"
	if len(exclusive) > 0 && exclusive[0] {
		strength = "UPDATE"
	}
	if err := tx.Clauses(clause.Locking{Strength: strength}).
		Where("id=? AND status=?", kb.TenantID, "active").
		First(&tenant).Error; err != nil {
		return "", err
	}
	return ordinaryDigest([]any{OrdinaryEmbeddingKBStamp(kb), model, tenant.RetrieverEngines}), nil
}

func ordinaryKnowledge(
	tx *gorm.DB,
	tenant uint64,
	id string,
	attempt int,
	finalizing ...bool,
) (*types.Knowledge, *types.KnowledgeBase, error) {
	var k types.Knowledge
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id=? AND tenant_id=?", id, tenant).
		First(&k).Error; err != nil {
		return nil, nil, err
	}
	statusOK := k.ParseStatus == types.ParseStatusProcessing ||
		(len(finalizing) > 0 && finalizing[0] &&
			(k.ParseStatus == types.ParseStatusFinalizing || k.ParseStatus == types.ParseStatusCompleted))
	if k.Channel != "web" || k.Type != "file" || k.FilePath == "" || !statusOK || attempt < 1 {
		return nil, nil, ErrOrdinaryEmbeddingChanged
	}
	var kb types.KnowledgeBase
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id=? AND tenant_id=?", k.KnowledgeBaseID, tenant).
		First(&kb).Error; err != nil {
		return nil, nil, err
	}
	if kb.HasVectorStore() || kb.PrivateOwnerID != "" {
		return nil, nil, ErrOrdinaryEmbeddingChanged
	}
	var latest int
	if err := tx.Table("knowledge_processing_spans").
		Select("COALESCE(MAX(attempt),0)").
		Where("knowledge_id=?", id).
		Scan(&latest).Error; err != nil {
		return nil, nil, err
	}
	if latest != attempt {
		return nil, nil, ErrOrdinaryEmbeddingChanged
	}
	var protected bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_id=?) `+
		`OR EXISTS(SELECT 1 FROM feishu_personal_resources WHERE logical_knowledge_id=?) `+
		`OR EXISTS(SELECT 1 FROM feishu_private_kbs WHERE kb_id=?)`, id, id, k.KnowledgeBaseID).
		Scan(&protected).Error; err != nil {
		return nil, nil, err
	}
	metadata, err := k.Metadata.Map()
	if err != nil {
		return nil, nil, err
	}
	if protected || metadata["datasource_id"] != nil || metadata["data_source_id"] != nil ||
		metadata["feishu_resource_id"] != nil {
		return nil, nil, ErrOrdinaryEmbeddingChanged
	}
	return &k, &kb, nil
}

// Start atomically accepts a verified manifest for one current parse attempt.
func (s *OrdinaryEmbeddingStore) Start(
	ctx context.Context,
	k *types.Knowledge,
	plan types.OrdinaryEmbeddingPlan,
) (*types.TaskPendingOp, error) {
	var op types.TaskPendingOp
	if k == nil || plan.Task.KnowledgeID != k.ID || plan.Task.TenantID != k.TenantID ||
		plan.Task.KnowledgeBaseID != k.KnowledgeBaseID {
		return nil, ErrOrdinaryEmbeddingChanged
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fresh, kb, err := ordinaryKnowledge(tx, k.TenantID, k.ID, plan.Task.Attempt)
		if err != nil {
			return err
		}
		if fresh.KnowledgeBaseID != plan.Task.KnowledgeBaseID || len(plan.Inputs) < 1000 || len(plan.Inputs) > 100000 ||
			plan.Dimension < 1 ||
			plan.StorageBytes < 0 {
			return ErrOrdinaryEmbeddingChanged
		}
		if ordinarySourceStamp(fresh) != ordinarySourceStamp(k) || OrdinaryEmbeddingKBStamp(kb) != plan.KBStamp {
			return ErrOrdinaryEmbeddingChanged
		}
		var existing types.TaskPendingOp
		err = tx.Where("scope=? AND scope_id=? AND tenant_id=? AND dedup_key=?",
			types.OrdinaryEmbeddingScope, k.ID, k.TenantID, fmt.Sprint(plan.Task.Attempt)).
			First(&existing).
			Error
		if err == nil {
			op = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		plan.SourceStamp = ordinarySourceStamp(fresh)
		plan.ConfigStamp, err = ordinaryConfigStamp(tx, kb)
		if err != nil {
			return err
		}
		plan.Version = 1
		plan.Cursor = 0
		plan.BatchAttempts = 0
		plan.LeaseToken = ""
		plan.LeaseUntil = time.Time{}
		// Check the complete manifest before accepting durable work.
		var rows []*types.Chunk
		if err := tx.Where("tenant_id=? AND knowledge_id=? AND chunk_type=? AND is_enabled",
			k.TenantID, k.ID, types.ChunkTypeText).
			Order("chunk_index,id").
			Find(&rows).Error; err != nil {
			return err
		}
		if err := ordinaryCheckManifest(rows, plan.Inputs); err != nil {
			return err
		}
		payload, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		op = types.TaskPendingOp{
			TenantID:   k.TenantID,
			TaskType:   types.TypeDocumentProcess,
			Scope:      types.OrdinaryEmbeddingScope,
			ScopeID:    k.ID,
			Op:         "pending",
			DedupKey:   fmt.Sprint(plan.Task.Attempt),
			Payload:    payload,
			EnqueuedAt: time.Now(),
		}
		return tx.Create(&op).Error
	})
	return &op, err
}

func ordinaryCheckManifest(rows []*types.Chunk, inputs []types.OrdinaryEmbeddingInput) error {
	if len(rows) != len(inputs) {
		return ErrOrdinaryEmbeddingChanged
	}
	stamps := make(map[string]string, len(inputs))
	for _, input := range inputs {
		if input.ChunkID == "" || len(input.SHA256) != sha256.Size*2 {
			return ErrOrdinaryEmbeddingChanged
		}
		if _, ok := stamps[input.ChunkID]; ok {
			return ErrOrdinaryEmbeddingChanged
		}
		stamps[input.ChunkID] = input.SHA256
	}
	for _, row := range rows {
		if !row.IsEnabled || row.ChunkType != types.ChunkTypeText ||
			stamps[row.ID] != OrdinaryEmbeddingChunkStamp(row) {
			return ErrOrdinaryEmbeddingChanged
		}
	}
	return nil
}

// Find locates an existing intent without changing its progress.
func (s *OrdinaryEmbeddingStore) Find(
	ctx context.Context,
	tenant uint64,
	id string,
	attempt int,
) (*types.TaskPendingOp, error) {
	var op types.TaskPendingOp
	err := s.db.WithContext(ctx).
		Where("scope=? AND scope_id=? AND tenant_id=? AND dedup_key=?",
			types.OrdinaryEmbeddingScope, id, tenant, fmt.Sprint(attempt)).
		First(&op).
		Error
	return &op, err
}

// DecodeOrdinaryEmbedding validates the server-owned continuation envelope.
func DecodeOrdinaryEmbedding(op *types.TaskPendingOp) (types.OrdinaryEmbeddingPlan, error) {
	var p types.OrdinaryEmbeddingPlan
	if op == nil || op.Scope != types.OrdinaryEmbeddingScope || op.TaskType != types.TypeDocumentProcess ||
		json.Unmarshal(op.Payload, &p) != nil ||
		p.Version != 1 ||
		p.Task.TenantID != op.TenantID ||
		p.Task.KnowledgeID != op.ScopeID ||
		p.Task.KnowledgeBaseID == "" ||
		p.Task.Attempt < 1 ||
		op.DedupKey != fmt.Sprint(p.Task.Attempt) ||
		len(p.Inputs) < 1000 ||
		len(p.Inputs) > 100000 ||
		p.Cursor < 0 ||
		p.Cursor > len(p.Inputs) ||
		p.BatchAttempts < 0 ||
		p.BatchAttempts > 4 ||
		p.Dimension < 1 ||
		p.StorageBytes < 0 ||
		len(p.SourceStamp) != 64 ||
		len(p.ConfigStamp) != 64 ||
		len(p.KBStamp) != 64 {
		return p, ErrOrdinaryEmbeddingChanged
	}
	if p.Task.PersonalAttemptID != "" || p.Task.DispatchID != "" || p.Task.FeishuAuthMode != "" || p.Task.FirstCreate ||
		p.Task.OrdinaryEmbeddingID != 0 ||
		p.Task.EmbeddingCursor != 0 ||
		(p.Cursor != len(p.Inputs) && p.Cursor%OrdinaryEmbeddingWindow != 0) {
		return p, ErrOrdinaryEmbeddingChanged
	}
	return p, nil
}

func (s *OrdinaryEmbeddingStore) checked(
	tx *gorm.DB,
	id int64,
	tenant uint64,
	cursor int,
	token string,
	exclusive ...bool,
) (*types.TaskPendingOp, types.OrdinaryEmbeddingPlan, *types.Knowledge, error) {
	var op types.TaskPendingOp
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id=? AND scope=? AND tenant_id=?", id, types.OrdinaryEmbeddingScope, tenant).
		First(&op).Error; err != nil {
		return nil, types.OrdinaryEmbeddingPlan{}, nil, err
	}
	p, err := DecodeOrdinaryEmbedding(&op)
	if err != nil {
		return nil, p, nil, err
	}
	if p.Cursor != cursor || (token != "" && (p.LeaseToken != token || !p.LeaseUntil.After(time.Now()))) {
		return nil, p, nil, ErrOrdinaryEmbeddingChanged
	}
	k, kb, err := ordinaryKnowledge(tx, tenant, op.ScopeID, p.Task.Attempt, op.Op == "finalizing")
	if err != nil {
		return nil, p, nil, err
	}
	config, err := ordinaryConfigStamp(tx, kb, exclusive...)
	if err != nil {
		return nil, p, nil, err
	}
	if config != p.ConfigStamp || ordinarySourceStamp(k) != p.SourceStamp {
		return nil, p, nil, ErrOrdinaryEmbeddingChanged
	}
	return &op, p, k, nil
}

func ordinarySavePlan(tx *gorm.DB, op *types.TaskPendingOp, p types.OrdinaryEmbeddingPlan) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return tx.Model(op).
		Updates(map[string]any{
			"payload": json.RawMessage(data), "op": op.Op,
			"claimed_at": op.ClaimedAt, "enqueued_at": time.Now(),
		}).
		Error
}

// Claim leases the next window and returns only its missing valid vectors.
func (s *OrdinaryEmbeddingStore) Claim(
	ctx context.Context,
	id int64,
	tenant uint64,
	cursor int,
) (*types.TaskPendingOp, types.OrdinaryEmbeddingPlan, []*types.Chunk, error) {
	var result *types.TaskPendingOp
	var plan types.OrdinaryEmbeddingPlan
	var chunks []*types.Chunk
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		op, p, _, err := s.checked(tx, id, tenant, cursor, "")
		if err != nil {
			return err
		}
		if op.Op == "completed" || op.Op == "attention" || op.Op == "cancelled" {
			return ErrOrdinaryEmbeddingChanged
		}
		if p.LeaseUntil.After(time.Now()) {
			return ErrOrdinaryEmbeddingBusy
		}
		if p.BatchAttempts >= 4 {
			return ErrOrdinaryEmbeddingChanged
		}
		p.BatchAttempts++
		p.LeaseToken = uuid.NewString()
		p.LeaseUntil = time.Now().Add(90 * time.Second)
		now := time.Now()
		op.ClaimedAt = &now
		op.Op = "processing"
		end := min(p.Cursor+OrdinaryEmbeddingWindow, len(p.Inputs))
		ids := make([]string, 0, end-p.Cursor)
		for _, item := range p.Inputs[p.Cursor:end] {
			ids = append(ids, item.ChunkID)
		}
		if len(ids) > 0 {
			if err := tx.Where("tenant_id=? AND knowledge_id=? AND id IN ?", tenant, op.ScopeID, ids).
				Find(&chunks).Error; err != nil {
				return err
			}
			if err := ordinaryCheckManifest(chunks, p.Inputs[p.Cursor:end]); err != nil {
				return err
			}
			var saved []string
			if err := tx.Table("embeddings").
				Where("knowledge_id=? AND knowledge_base_id=? AND source_id IN ? AND chunk_id=source_id "+
					"AND source_type=? AND is_enabled AND dimension=? AND vector_dims(embedding)=?",
					op.ScopeID, p.Task.KnowledgeBaseID, ids, types.ChunkSourceType, p.Dimension, p.Dimension).
				Pluck("source_id", &saved).Error; err != nil {
				return err
			}
			persisted := map[string]bool{}
			for _, id := range saved {
				persisted[id] = true
			}
			missing := make([]*types.Chunk, 0, len(chunks))
			for _, chunk := range chunks {
				if !persisted[chunk.ID] {
					missing = append(missing, chunk)
				}
			}
			chunks = missing
		}
		if err := ordinarySavePlan(tx, op, p); err != nil {
			return err
		}
		result = op
		plan = p
		return nil
	})
	return result, plan, chunks, err
}

// WithWrite holds the plan, knowledge, configuration and input rows throughout
// the PG index write. Cancel/reparse/edit cannot interleave after validation.
func (s *OrdinaryEmbeddingStore) WithWrite(
	ctx context.Context,
	db *gorm.DB,
	id int64,
	tenant uint64,
	cursor int,
	token string,
	write func(*gorm.DB) error,
) error {
	pool, poolErr := db.DB()
	ownerPool, ownerErr := s.db.DB()
	if poolErr != nil || ownerErr != nil || pool != ownerPool || db.Name() != s.db.Name() {
		return ErrOrdinaryEmbeddingChanged
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		op, p, _, err := s.checked(tx, id, tenant, cursor, token)
		if err != nil {
			return err
		}
		if op.Op != "processing" {
			return ErrOrdinaryEmbeddingChanged
		}
		end := min(cursor+OrdinaryEmbeddingWindow, len(p.Inputs))
		ids := make([]string, 0, end-cursor)
		for _, item := range p.Inputs[cursor:end] {
			ids = append(ids, item.ChunkID)
		}
		var chunks []*types.Chunk
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("tenant_id=? AND knowledge_id=? AND id IN ?", tenant, op.ScopeID, ids).
			Find(&chunks).Error; err != nil {
			return err
		}
		if err := ordinaryCheckManifest(chunks, p.Inputs[cursor:end]); err != nil {
			return err
		}
		return write(tx)
	})
}

// Renew extends only the current consumer and keeps the document heartbeat live.
func (s *OrdinaryEmbeddingStore) Renew(ctx context.Context, id int64, tenant uint64, cursor int, token string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		op, p, k, err := s.checked(tx, id, tenant, cursor, token)
		if err != nil {
			return err
		}
		p.LeaseUntil = time.Now().Add(90 * time.Second)
		if err := ordinarySavePlan(tx, op, p); err != nil {
			return err
		}
		return tx.Model(k).
			UpdateColumn("updated_at", time.Now()).Error
	})
}

// Advance persists progress only after proving complete window vector coverage.
func (s *OrdinaryEmbeddingStore) Advance(
	ctx context.Context,
	id int64,
	tenant uint64,
	cursor int,
	token string,
) (*types.TaskPendingOp, error) {
	var result *types.TaskPendingOp
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		op, p, k, err := s.checked(tx, id, tenant, cursor, token)
		if err != nil {
			return err
		}
		end := min(cursor+OrdinaryEmbeddingWindow, len(p.Inputs))
		// A successful provider call without all PG rows is not durable progress.
		ids := make([]string, 0, end-cursor)
		for _, item := range p.Inputs[cursor:end] {
			ids = append(ids, item.ChunkID)
		}
		if len(ids) > 0 {
			var count int64
			if err := tx.Table("embeddings").
				Where("knowledge_id=? AND knowledge_base_id=? AND source_id IN ? AND chunk_id=source_id "+
					"AND source_type=? AND is_enabled AND dimension=? AND vector_dims(embedding)=?",
					k.ID, k.KnowledgeBaseID, ids, types.ChunkSourceType, p.Dimension, p.Dimension).
				Count(&count).Error; err != nil {
				return err
			}
			if count != int64(len(ids)) {
				return ErrOrdinaryEmbeddingChanged
			}
		}
		p.Cursor = end
		p.BatchAttempts = 0
		p.LeaseToken = ""
		p.LeaseUntil = time.Time{}
		op.ClaimedAt = nil
		op.Op = "pending"
		if end == len(p.Inputs) {
			op.Op = "indexed"
		}
		if err := ordinarySavePlan(tx, op, p); err != nil {
			return err
		}
		result = op
		return tx.Model(k).
			UpdateColumn("updated_at", time.Now()).Error
	})
	return result, err
}

// Release retains partial results and clears only the specified consumer lease.
func (s *OrdinaryEmbeddingStore) Release(
	ctx context.Context,
	id int64,
	tenant uint64,
	cursor int,
	token string,
	permanent bool,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var op types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id=? AND scope=? AND tenant_id=?", id, types.OrdinaryEmbeddingScope, tenant).
			First(&op).Error; err != nil {
			return err
		}
		p, err := DecodeOrdinaryEmbedding(&op)
		if err != nil {
			return err
		}
		if p.Cursor != cursor || p.LeaseToken != token {
			return ErrOrdinaryEmbeddingChanged
		}
		p.LeaseToken = ""
		p.LeaseUntil = time.Time{}
		op.ClaimedAt = nil
		op.Op = "pending"
		p.LastError = "temporary_index_failure"
		if permanent || p.BatchAttempts >= 4 {
			op.Op = "attention"
			p.LastError = "index_recovery_requires_review"
		}
		if err := ordinarySavePlan(tx, &op, p); err != nil {
			return err
		}
		if op.Op == "attention" {
			return ordinaryFailCurrent(tx, &op, p)
		}
		return nil
	})
}

// List scans recoverable intents while excluding currently live consumers.
func (s *OrdinaryEmbeddingStore) List(ctx context.Context, after int64, limit int) ([]*types.TaskPendingOp, error) {
	var ops []*types.TaskPendingOp
	err := s.db.WithContext(ctx).
		Where("scope=? AND id>? AND op IN ?", types.OrdinaryEmbeddingScope, after,
			[]string{"pending", "processing", "indexed", "finalizing"}).
		Where("op <> 'processing' OR COALESCE(NULLIF(payload->>'lease_until','')::timestamptz," +
			"'-infinity'::timestamptz) <= CURRENT_TIMESTAMP").
		Order("id").
		Limit(min(limit, 100)).
		Find(&ops).
		Error
	return ops, err
}

// Finish verifies the whole manifest once more, charges storage exactly once,
// and leaves completion to the normal post-processing orchestrator.
func (s *OrdinaryEmbeddingStore) Finish(ctx context.Context, id int64, tenant uint64) (*types.Knowledge, error) {
	var result *types.Knowledge
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var anchor types.TaskPendingOp
		if err := tx.Where("id=? AND scope=? AND tenant_id=?", id, types.OrdinaryEmbeddingScope, tenant).
			First(&anchor).Error; err != nil {
			return err
		}
		plan, err := DecodeOrdinaryEmbedding(&anchor)
		if err != nil {
			return err
		}
		op, p, k, err := s.checked(tx, id, tenant, len(plan.Inputs), "", true)
		if err != nil {
			return err
		}
		if op.Op == "finalizing" || op.Op == "completed" {
			result = k
			return nil
		}
		if op.Op != "indexed" || p.Cursor != len(p.Inputs) {
			return ErrOrdinaryEmbeddingChanged
		}
		var chunks []*types.Chunk
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("tenant_id=? AND knowledge_id=? AND chunk_type=? AND is_enabled", tenant, k.ID, types.ChunkTypeText).
			Find(&chunks).Error; err != nil {
			return err
		}
		if err := ordinaryCheckManifest(chunks, p.Inputs); err != nil {
			return err
		}
		var count int64
		if err := tx.Table("embeddings e").
			Joins("JOIN chunks c ON c.id=e.source_id AND c.knowledge_id=e.knowledge_id").
			Where("e.knowledge_id=? AND e.knowledge_base_id=? AND e.chunk_id=e.source_id AND e.source_type=? "+
				"AND e.is_enabled AND e.dimension=? AND vector_dims(e.embedding)=? "+
				"AND c.tenant_id=? AND c.chunk_type=? AND c.deleted_at IS NULL AND c.is_enabled",
				k.ID, k.KnowledgeBaseID, types.ChunkSourceType, p.Dimension, p.Dimension, tenant, types.ChunkTypeText).
			Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(chunks)) {
			return ErrOrdinaryEmbeddingChanged
		}
		var embeddingModelID string
		if err := tx.Model(&types.KnowledgeBase{}).
			Select("embedding_model_id").
			Where("id=?", k.KnowledgeBaseID).
			Scan(&embeddingModelID).Error; err != nil {
			return err
		}
		now := time.Now()
		k.StorageSize = p.StorageBytes
		k.EmbeddingModelID = embeddingModelID
		k.EnableStatus = "enabled"
		k.SummaryStatus = types.SummaryStatusNone
		k.ProcessedAt = &now
		k.UpdatedAt = now
		if err := tx.Model(k).
			Updates(map[string]any{
				"storage_size": k.StorageSize, "embedding_model_id": k.EmbeddingModelID,
				"enable_status": k.EnableStatus, "summary_status": k.SummaryStatus,
				"processed_at": now, "updated_at": now,
			}).Error; err != nil {
			return err
		}
		charge := tx.Model(&types.Tenant{}).
			Where("id=? AND (storage_quota<=0 OR storage_used+?<=storage_quota)", tenant, p.StorageBytes).
			UpdateColumn("storage_used", gorm.Expr("storage_used + ?", p.StorageBytes))
		if charge.Error != nil {
			return charge.Error
		}
		if charge.RowsAffected != 1 {
			return ErrOrdinaryEmbeddingChanged
		}
		op.Op = "finalizing"
		if err := ordinarySavePlan(tx, op, p); err != nil {
			return err
		}
		result = k
		return nil
	})
	return result, err
}

// DispatchedPostprocess closes an intent after normal postprocess admission.
func (s *OrdinaryEmbeddingStore) DispatchedPostprocess(ctx context.Context, id int64, tenant uint64) error {
	// Queue acknowledgement alone is not processing acknowledgement. Keep
	// recovery live until the normal orchestrator has admitted this attempt.
	return s.db.WithContext(ctx).Model(&types.TaskPendingOp{}).
		Where("id=? AND scope=? AND tenant_id=? AND op=?", id, types.OrdinaryEmbeddingScope, tenant, "finalizing").
		Where(`EXISTS (SELECT 1 FROM knowledges k WHERE k.id=task_pending_ops.scope_id `+
			`AND k.tenant_id=task_pending_ops.tenant_id AND k.deleted_at IS NULL AND k.parse_status IN ? `+
			`AND (SELECT COALESCE(MAX(s.attempt),0) FROM knowledge_processing_spans s `+
			`WHERE s.knowledge_id=k.id)=CAST(task_pending_ops.dedup_key AS INTEGER))`,
			[]string{types.ParseStatusFinalizing, types.ParseStatusCompleted}).
		UpdateColumn("op", "completed").Error
}

// Reject records only an expired/unowned plan. It never changes a newer parse,
// a cancelled/deleted document, another consumer's lease, or any source ACL.
func (s *OrdinaryEmbeddingStore) Reject(ctx context.Context, id int64, tenant uint64, cursor int) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var op types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id=? AND tenant_id=? AND scope=?", id, tenant, types.OrdinaryEmbeddingScope).
			First(&op).Error; err != nil {
			return err
		}
		if op.Op == "completed" || op.Op == "cancelled" || op.Op == "attention" {
			return nil
		}
		p, err := DecodeOrdinaryEmbedding(&op)
		if err != nil {
			return err
		}
		if p.Cursor != cursor || p.LeaseUntil.After(time.Now()) {
			return nil
		}
		op.Op = "attention"
		op.ClaimedAt = nil
		p.LeaseToken = ""
		p.LastError = "index_recovery_requires_review"
		if err := ordinarySavePlan(tx, &op, p); err != nil {
			return err
		}
		return ordinaryFailCurrent(tx, &op, p)
	})
}

func ordinaryFailCurrent(tx *gorm.DB, op *types.TaskPendingOp, p types.OrdinaryEmbeddingPlan) error {
	var k types.Knowledge
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id=? AND tenant_id=?", op.ScopeID, op.TenantID).
		First(&k).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if k.ParseStatus != types.ParseStatusProcessing {
		return nil
	}
	var latest int
	if err := tx.Table("knowledge_processing_spans").
		Select("COALESCE(MAX(attempt),0)").
		Where("knowledge_id=?", op.ScopeID).
		Scan(&latest).Error; err != nil {
		return err
	}
	if latest != p.Task.Attempt {
		return nil
	}
	if _, _, err := ordinaryKnowledge(tx, op.TenantID, op.ScopeID, p.Task.Attempt); err != nil {
		if errors.Is(err, ErrOrdinaryEmbeddingChanged) || errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if err := k.SetParseFailure(&types.KnowledgeParseFailure{
		Code: "vector_store_write_failed", State: "needs_attention", Action: "review_processing_trace",
		Attempts: max(1, p.BatchAttempts), MaxAttempts: 4,
	}); err != nil {
		return err
	}
	return tx.Model(&k).
		Updates(map[string]any{
			"parse_status": types.ParseStatusFailed, "metadata": k.Metadata,
			"error_message": "索引任务无法安全续跑，已保留解析内容和完成的批次，请查看处理记录。",
			"updated_at":    time.Now(),
		}).
		Error
}
