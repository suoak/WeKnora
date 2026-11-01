package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type exactRetryStage struct {
	SHA256          string
	Chunks, Vectors int64
	PlaintextBytes  int64
	KnowledgeEmpty  bool
	InvalidChunks   int64
	InvalidVectors  int64
	ExternalLinks   bool
	CascadingDelete bool
}

// The inventory returns hashes/cardinality only. Old derived bodies are not
// source evidence and must never be sent to a parser, model or content API.
func inspectExactRetryStage(
	tx *gorm.DB, source types.PersonalExactReceipt, lock bool,
) (*exactRetryStage, error) {
	if source.TenantID == 0 || source.KnowledgeID == "" || source.KnowledgeBaseID == "" {
		return nil, repository.ErrPersonalIdentity
	}
	if lock {
		for _, query := range []string{
			"SELECT id FROM knowledges WHERE id=? FOR UPDATE",
			"SELECT id FROM chunks WHERE knowledge_id=? ORDER BY id FOR UPDATE",
			"SELECT id FROM embeddings WHERE knowledge_id=? ORDER BY id FOR UPDATE",
		} {
			var ids []string
			if err := tx.Raw(query, source.KnowledgeID).Scan(&ids).Error; err != nil {
				return nil, err
			}
		}
	}
	var stage exactRetryStage
	err := tx.Raw(`WITH c AS (SELECT * FROM chunks WHERE knowledge_id=?),
e AS (SELECT * FROM embeddings WHERE knowledge_id=?)
SELECT
(SELECT count(*) FROM c) AS chunks,(SELECT count(*) FROM e) AS vectors,
COALESCE((SELECT sum(octet_length(to_jsonb(c)::text)) FROM c),0)+
COALESCE((SELECT sum(octet_length(to_jsonb(e)::text)) FROM e),0) AS plaintext_bytes,
EXISTS(SELECT 1 FROM knowledges k WHERE k.id=? AND k.tenant_id=? AND k.knowledge_base_id=?
 AND k.deleted_at IS NULL AND k.enable_status='disabled'
 AND k.parse_status IN ('pending','processing') AND COALESCE(k.description,'')=''
 AND (k.profile IS NULL OR k.profile::text='null')
 AND (k.custom_metadata IS NULL OR k.custom_metadata::text IN ('{}','null'))) AS knowledge_empty,
(SELECT count(*) FROM c WHERE tenant_id IS DISTINCT FROM ? OR knowledge_base_id IS DISTINCT FROM ?
 OR is_enabled IS DISTINCT FROM FALSE OR deleted_at IS NOT NULL OR content_revision IS DISTINCT FROM 0
 OR last_editor_id IS DISTINCT FROM '' OR chunk_type IS NULL
 OR chunk_type NOT IN ('text','parent_text')) AS invalid_chunks,
(SELECT count(*) FROM e WHERE knowledge_base_id IS DISTINCT FROM ?
 OR is_enabled IS DISTINCT FROM FALSE OR source_type IS DISTINCT FROM ?
 OR source_id IS NULL OR source_id NOT IN (SELECT id FROM c)
 OR (COALESCE(chunk_id,'')<>'' AND chunk_id NOT IN (SELECT id FROM c))) AS invalid_vectors,
EXISTS(SELECT 1 FROM chunks other JOIN c ON other.knowledge_id<>c.knowledge_id
 AND (other.pre_chunk_id=c.id OR other.next_chunk_id=c.id OR other.parent_chunk_id=c.id
 OR position(c.id in COALESCE(other.relation_chunks::text,''))>0
 OR position(c.id in COALESCE(other.indirect_relation_chunks::text,''))>0)) OR
EXISTS(SELECT 1 FROM embeddings other JOIN c ON other.knowledge_id<>c.knowledge_id
 AND (other.source_id=c.id OR other.chunk_id=c.id)) AS external_links,
EXISTS(SELECT 1 FROM pg_constraint WHERE contype='f' AND confrelid IN
 ('chunks'::regclass,'embeddings'::regclass) AND confdeltype IN ('c','n','d')) AS cascading_delete,
encode(sha256(convert_to(jsonb_build_object(
 'knowledge',(SELECT encode(sha256(convert_to(to_jsonb(k)::text,'UTF8')),'hex') FROM knowledges k WHERE k.id=?),
 'chunks',(SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'hash',
  encode(sha256(convert_to(to_jsonb(c)::text,'UTF8')),'hex')) ORDER BY id),'[]'::jsonb) FROM c),
 'vectors',(SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'hash',
  encode(sha256(convert_to(to_jsonb(e)::text,'UTF8')),'hex')) ORDER BY id),'[]'::jsonb) FROM e)
)::text,'UTF8')),'hex') AS sha256`, source.KnowledgeID, source.KnowledgeID,
		source.KnowledgeID, source.TenantID, source.KnowledgeBaseID, source.TenantID, source.KnowledgeBaseID,
		source.KnowledgeBaseID, int(types.ChunkSourceType), source.KnowledgeID).Scan(&stage).Error
	if err != nil {
		return nil, err
	}
	if !stage.KnowledgeEmpty || stage.Chunks < 1 || stage.Chunks > 5000 || stage.Vectors > 5000 ||
		stage.PlaintextBytes > 16<<20 || stage.InvalidChunks != 0 || stage.InvalidVectors != 0 ||
		stage.ExternalLinks || stage.CascadingDelete || len(stage.SHA256) != 64 {
		return nil, fmt.Errorf("exact_retry_stage_requires_review: %w", repository.ErrPersonalFence)
	}
	if tx.Migrator().HasTable(&types.ChunkRevision{}) {
		var edits int64
		if err := tx.Table("chunk_revisions r").Joins("JOIN chunks c ON c.id=r.chunk_id").
			Where("c.knowledge_id=?", source.KnowledgeID).Count(&edits).Error; err != nil {
			return nil, err
		}
		if edits != 0 {
			return nil, repository.ErrPersonalFence
		}
	}
	return &stage, nil
}

func loadExactRetryStagingReceipt(
	tx *gorm.DB, expected types.PersonalExactReceipt, lock bool,
) (*types.TaskPendingOp, error) {
	var rows []types.TaskPendingOp
	query := tx.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope, expected.KnowledgeID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Limit(2).Find(&rows).Error; err != nil {
		return nil, err
	}
	var stored types.PersonalExactReceipt
	if len(rows) != 1 || rows[0].Op != "staging" || rows[0].TenantID != expected.TenantID ||
		json.Unmarshal(rows[0].Payload, &stored) != nil || stored != expected {
		return nil, repository.ErrPersonalFence
	}
	return &rows[0], nil
}

// Preserve the complete OLD, UNVERIFIED stage as encrypted quarantine before
// removing it from active storage. This function returns no old plaintext and
// issues no read capability. Published, edited, externally referenced, mixed
// or guarded data is rejected. Existing database write triggers remain active.
func (p *PersonalRecovery) quarantineExactRetryStage(
	tx *gorm.DB, proof *personalExactDocument, intent types.PersonalExactRetry, ticketID int64,
) error {
	if proof == nil || proof.generation != intent.SealedGeneration+1 || proof.nonce == "" || ticketID <= 0 {
		return repository.ErrPersonalFence
	}
	if err := p.commitExactPersonalDocument(tx, proof); err != nil {
		return err
	}
	stage, err := inspectExactRetryStage(tx, intent.Source, true)
	if err != nil || stage.SHA256 != intent.StageSHA256 {
		return repository.ErrPersonalFence
	}
	receipt, err := loadExactRetryStagingReceipt(tx, intent.Source, true)
	if err != nil {
		return err
	}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil || receipt.ID != intent.SourceReceiptID || exactRetrySHA(receiptBytes) != intent.SourceReceiptSHA {
		return repository.ErrPersonalFence
	}
	var archives int64
	key := fmt.Sprintf("%s:%d", proof.knowledge, intent.SealedGeneration)
	if err := tx.Model(&types.TaskPendingOp{}).Where("scope=? AND scope_id=?",
		types.PersonalExactArchiveScope, key).Count(&archives).Error; err != nil {
		return err
	}
	if archives != 0 {
		return repository.ErrPersonalFence
	}
	// Only the encryption routine receives the private old rows; no caller,
	// logger, content response, prompt builder or model sees this plaintext.
	ciphertext, digest, err := sealExactRetryStage(tx, intent, ticketID, receipt)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"version": 1, "purpose": "unverified_staging_quarantine", "ticket_id": ticketID,
		"knowledge_id": proof.knowledge, "tenant_id": proof.tenant,
		"sealed_generation": intent.SealedGeneration, "replacement_generation": proof.generation,
		"plaintext_sha256": digest, "ciphertext": ciphertext, "chunks": stage.Chunks, "vectors": stage.Vectors,
	})
	if err != nil {
		return err
	}
	if err := tx.Create(&types.TaskPendingOp{
		TenantID: proof.tenant, Scope: types.PersonalExactArchiveScope,
		ScopeID: key, TaskType: "knowledge:exact_archive", Op: "retained", DedupKey: key, Payload: payload,
		EnqueuedAt: time.Now().UTC(),
	}).Error; err != nil {
		return err
	}
	for _, change := range []struct {
		query string
		args  []any
		rows  int64
	}{
		{
			"DELETE FROM embeddings WHERE knowledge_id=? AND knowledge_base_id=?",
			[]any{proof.knowledge, proof.kb},
			stage.Vectors,
		},
		{
			"DELETE FROM chunks WHERE knowledge_id=? AND knowledge_base_id=? AND tenant_id=?",
			[]any{proof.knowledge, proof.kb, proof.tenant},
			stage.Chunks,
		},
		{
			"DELETE FROM task_pending_ops WHERE id=? AND scope=? AND op='staging'",
			[]any{receipt.ID, types.PersonalExactReceiptScope},
			1,
		},
	} {
		result := tx.Exec(change.query, change.args...)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != change.rows {
			return repository.ErrPersonalFence
		}
	}
	return p.commitExactPersonalDocument(tx, proof)
}

func sealExactRetryStage(
	tx *gorm.DB, intent types.PersonalExactRetry, ticketID int64, receipt *types.TaskPendingOp,
) (string, string, error) {
	var raw string
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		return "", "", err
	}
	err = tx.Raw(`SELECT jsonb_build_object(
 'purpose','personal_exact_unverified_stage_v1','ticket_id',?::bigint,
 'tenant_id',?::bigint,'knowledge_id',?::text,'sealed_generation',?::bigint,
 'original_receipt',?::jsonb,
 'chunks',(SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY c.id),'[]'::jsonb) FROM chunks c WHERE c.knowledge_id=?),
 'vectors',(SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.id),'[]'::jsonb) FROM embeddings e WHERE e.knowledge_id=?)
)::text`, ticketID, intent.Source.TenantID, intent.Source.KnowledgeID, intent.SealedGeneration,
		string(receiptBytes), intent.Source.KnowledgeID, intent.Source.KnowledgeID).Scan(&raw).Error
	if err != nil || len(raw) > 32<<20 {
		return "", "", repository.ErrPersonalFence
	}
	// sealFeishu marshals RawMessage into compact JSON. Hash the exact bytes
	// that are encrypted, so retained evidence can be verified after decrypt.
	canonical, err := json.Marshal(json.RawMessage(raw))
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(canonical)
	sealed, err := sealFeishu(json.RawMessage(canonical))
	return sealed, hex.EncodeToString(sum[:]), err
}

func exactRetrySHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
