package repository

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WikiParsedStallCandidate struct {
	ID, KnowledgeHash, ChunkHash, IndexHash, OriginalError string
	TextChunks, IndexChunks, Attempt                       int64
	PipelineDone, Unsafe                                   bool
}

// WikiParsedStallPlan contains no bodies, credentials or generated authority.
type WikiParsedStallPlan struct {
	TenantID          uint64                     `json:"tenant_id"`
	KnowledgeBaseID   string                     `json:"knowledge_base_id"`
	Documents         []WikiParsedStallCandidate `json:"documents"`
	PendingOperations int                        `json:"pending_operations"`
	Snapshot          string                     `json:"snapshot"`
	Applied           bool                       `json:"applied"`
}

// PrepareWikiParsedStallRecovery restores only completed native PG parses that
// housekeeping misclassified while waiting for Wiki. It never marks completion,
// regenerates an index, modifies an old operation, or certifies an old Wiki page.
// The operator holds the same KB wikilock as normal generation.
func PrepareWikiParsedStallRecovery(ctx context.Context, db *gorm.DB, tenant uint64, kb, expected string,
	apply bool,
) (*WikiParsedStallPlan, error) {
	if db.Name() != "postgres" || tenant == 0 || kb == "" || (apply && expected == "") {
		return nil, errors.New("explicit native PostgreSQL scope and apply snapshot required")
	}
	var plan *WikiParsedStallPlan
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sources []struct{ ID string }
		if err := tx.Table("data_sources").Select("id").Where("tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL",
			tenant, kb).Order("id").Clauses(clause.Locking{Strength: "UPDATE"}).Scan(&sources).Error; err != nil {
			return err
		}
		var anchor types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", kb, tenant).Take(&anchor).Error; err != nil {
			return err
		}
		if !anchor.IsWikiEnabled() || anchor.VectorStoreID != nil {
			return errors.New("Wiki enabled local native index required")
		}
		var busy int64
		if err := tx.Table("sync_logs").Where("data_source_id IN (SELECT id FROM data_sources WHERE knowledge_base_id=?) "+
			"AND (status IN ('running','pending') OR execution_claimed)", kb).Count(&busy).Error; err != nil {
			return err
		}
		if busy != 0 {
			return errors.New("source execution still active")
		}
		var locked []struct{ ID string }
		if err := tx.Table("knowledges").Select("id").Where("knowledge_base_id=? AND tenant_id=? AND deleted_at IS NULL",
			kb, tenant).Order("id").Clauses(clause.Locking{Strength: "UPDATE"}).Scan(&locked).Error; err != nil {
			return err
		}
		var rows []WikiParsedStallCandidate
		err := tx.Raw(`SELECT k.id,md5(to_jsonb(k)::text) AS knowledge_hash,k.error_message AS original_error,
 (SELECT md5(coalesce(string_agg(md5(to_jsonb(c)::text),'' ORDER BY c.id),'')) FROM chunks c
  WHERE c.knowledge_id=k.id) AS chunk_hash,
 (SELECT md5(coalesce(string_agg(md5(to_jsonb(e)::text),'' ORDER BY e.id),'')) FROM embeddings e
  WHERE e.knowledge_id=k.id) AS index_hash,
 (SELECT count(*) FROM chunks c WHERE c.knowledge_id=k.id AND c.deleted_at IS NULL
  AND c.chunk_type='text' AND c.is_enabled AND trim(c.content)<>'') AS text_chunks,
 (SELECT count(DISTINCT c.id) FROM chunks c JOIN embeddings e ON e.chunk_id=c.id
  AND e.source_id=c.id AND e.source_type=? AND e.knowledge_id=k.id AND e.knowledge_base_id=k.knowledge_base_id
  AND e.is_enabled AND e.embedding IS NOT NULL AND e.dimension>0
  WHERE c.knowledge_id=k.id AND c.deleted_at IS NULL AND c.chunk_type='text' AND c.is_enabled
  AND trim(c.content)<>'' AND c.index_status='ready') AS index_chunks,
 (SELECT coalesce(max(attempt),0) FROM knowledge_processing_spans s WHERE s.knowledge_id=k.id) AS attempt,
 (SELECT count(DISTINCT name)=3 FROM knowledge_processing_spans s WHERE s.knowledge_id=k.id
  AND s.attempt=(SELECT coalesce(max(attempt),0) FROM knowledge_processing_spans z WHERE z.knowledge_id=k.id)
  AND s.name IN ('docreader','chunking','embedding') AND s.status='done') AS pipeline_done,
 EXISTS(SELECT 1 FROM chunks c WHERE c.knowledge_id=k.id AND c.deleted_at IS NULL
  AND (c.tenant_id<>k.tenant_id OR c.knowledge_base_id<>k.knowledge_base_id OR
   (c.is_enabled AND c.index_status<>'ready'))) OR
 EXISTS(SELECT 1 FROM knowledge_processing_spans s WHERE s.knowledge_id=k.id AND s.status IN ('running','pending'))
 AS unsafe
 FROM knowledges k WHERE k.knowledge_base_id=? AND k.tenant_id=? AND k.deleted_at IS NULL
 AND k.parse_status='failed' AND k.channel='gitlab' AND k.enable_status='enabled'
 AND k.summary_status='completed' AND k.pending_subtasks_count=0
 AND k.error_message LIKE 'task stuck in finalizing:%recovered by housekeeping'
 ORDER BY k.id`, types.ChunkSourceType, kb, tenant).Scan(&rows).Error
		if err != nil {
			return err
		}
		if len(rows) == 0 || len(rows) > 2048 {
			return errors.New("no bounded verified parsed stalls")
		}
		parsed := map[string]bool{}
		for _, row := range rows {
			if row.Unsafe || !row.PipelineDone || row.TextChunks < 1 || row.TextChunks != row.IndexChunks {
				return errors.New("parsed pipeline or native index evidence incomplete")
			}
			parsed[row.ID] = true
		}
		var pending []types.TaskPendingOp
		if err := tx.Where("scope='knowledge_base' AND scope_id=?", kb).Order("id").
			Clauses(clause.Locking{Strength: "UPDATE"}).Find(&pending).Error; err != nil {
			return err
		}
		for _, op := range pending {
			if op.TenantID != tenant || op.ClaimedAt != nil ||
				(op.TaskType != types.TypeWikiIngest && op.TaskType != types.TypeWikiFinalize) {
				return errors.New("existing Wiki operation is claimed or outside scope")
			}
		}
		proof, err := wikiDependenciesWithParsedRecovery(tx, kb, parsed)
		if err != nil {
			return err
		}
		stamps, err := wikiDocumentInputStamps(tx, kb, tenant)
		if err != nil {
			return err
		}
		plan = &WikiParsedStallPlan{
			TenantID:          tenant,
			KnowledgeBaseID:   kb,
			Documents:         rows,
			PendingOperations: len(pending),
		}
		plan.Snapshot = wikiDigest([]any{anchor, rows, pending, proof.Sources, proof.Epochs, stamps})
		if !apply {
			return nil
		}
		if expected != plan.Snapshot {
			return errors.New("parsed recovery snapshot changed")
		}
		now := time.Now().UTC()
		for _, row := range rows {
			result := tx.Table("knowledges").Where("id=? AND tenant_id=? AND knowledge_base_id=? "+
				"AND deleted_at IS NULL AND parse_status='failed' AND md5(to_jsonb(knowledges)::text)=?",
				row.ID, tenant, kb, row.KnowledgeHash).Updates(map[string]any{
				"parse_status": types.ParseStatusFinalizing, "pending_subtasks_count": 1,
				"error_message": WikiRegenerationRequiredMessage, "updated_at": now,
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("parsed recovery document changed")
			}
			payload, err := json.Marshal(
				map[string]any{
					"op":           "ingest",
					"knowledge_id": row.ID,
					"attempt":      row.Attempt,
					"language":     "zh-CN",
				},
			)
			if err != nil {
				return err
			}
			if err := tx.Create(&types.TaskPendingOp{
				TenantID: tenant, TaskType: types.TypeWikiIngest,
				Scope: types.TaskScopeKnowledgeBase, ScopeID: kb, Op: "ingest", DedupKey: row.ID, Payload: payload, EnqueuedAt: now,
			}).Error; err != nil {
				return err
			}
		}
		// The full ordinary proof must succeed after the status transition. Recovery
		// never passes its exceptional preview set into the worker's execution.
		fresh, err := wikiDependencies(tx, kb)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(proof.Sources, fresh.Sources) {
			return errors.New("parsed content changed during recovery")
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		var verified []struct{ ID, ChunkHash, IndexHash string }
		if err := tx.Raw(`SELECT k.id,
 (SELECT md5(coalesce(string_agg(md5(to_jsonb(c)::text),'' ORDER BY c.id),'')) FROM chunks c
  WHERE c.knowledge_id=k.id) AS chunk_hash,
 (SELECT md5(coalesce(string_agg(md5(to_jsonb(e)::text),'' ORDER BY e.id),'')) FROM embeddings e
  WHERE e.knowledge_id=k.id) AS index_hash
 FROM knowledges k WHERE k.id IN ? ORDER BY k.id`, ids).Scan(&verified).Error; err != nil {
			return err
		}
		if len(verified) != len(rows) {
			return errors.New("parsed index binding changed")
		}
		for i, row := range rows {
			if verified[i].ID != row.ID || verified[i].ChunkHash != row.ChunkHash ||
				verified[i].IndexHash != row.IndexHash {
				return errors.New("parsed index changed during recovery")
			}
		}
		plan.Applied = true
		audit, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		return tx.Create(&types.TaskPendingOp{
			TenantID: tenant, TaskType: "wiki:parsed_recovery",
			Scope: "wiki_parsed_recovery_audit", ScopeID: kb, Op: "applied", DedupKey: plan.Snapshot, Payload: audit, EnqueuedAt: now,
		}).Error
	})
	return plan, err
}
