package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type exactPublishedVector struct {
	SourceID, ChunkID, KnowledgeID, KnowledgeBaseID, Content string
	SourceType, Dimension                                    int
	Embedding                                                pgvector.HalfVector
	IsEnabled                                                bool
}

// Publishing is separate from ordinary scoped writes: the final CAS consumes
// the claim, so a post-write check requiring a live processing nonce would be
// incorrect. All data changes and the receipt commit in the canonical owner
// transaction, after validating that every stored output is a known result.
func (c *exactPersonalConsumer) publish(ctx context.Context) error {
	if !c.indexed || !c.derivedReady || c.parsed == nil || len(c.outputs) == 0 || len(c.indexContents) == 0 {
		return errPersonalExactDocument
	}
	fresh, err := c.recovery.revalidateExactPersonalDocument(ctx, c.source)
	if err != nil {
		return err
	}
	owner := &types.FeishuPersonalAttempt{
		ID: fresh.attempt, Fence: fresh.generation, OwnerNonce: fresh.nonce,
		CandidateVersionID: fresh.candidate, ExpectedActiveVersion: fresh.expectedActive,
	}
	return c.recovery.mutate(ctx, owner, 0, func(tx *gorm.DB, attempt *types.FeishuPersonalAttempt) error {
		if err := c.recovery.commitExactPersonalDocument(tx, fresh); err != nil {
			return err
		}
		var receipts []types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"scope=? AND scope_id=? AND tenant_id=?", types.PersonalExactReceiptScope, fresh.knowledge, fresh.tenant,
		).Limit(2).Find(&receipts).Error; err != nil {
			return err
		}
		if len(receipts) != 1 {
			return repository.ErrPersonalFence
		}
		receipt := receipts[0]
		var source types.PersonalExactReceipt
		if receipt.Op != "staging" || json.Unmarshal(receipt.Payload, &source) != nil ||
			source != exactSourceReceipt(fresh) {
			return repository.ErrPersonalFence
		}
		// Count before reading any derived body. Unknown output is preserved and
		// rejected, never adopted or deleted by a source-file proof.
		var chunkCount, vectorCount int64
		if err := tx.Table("chunks").Where("knowledge_id=?", fresh.knowledge).Count(&chunkCount).Error; err != nil {
			return err
		}
		if err := tx.Table("embeddings").Where("knowledge_id=?", fresh.knowledge).
			Count(&vectorCount).Error; err != nil {
			return err
		}
		if chunkCount != int64(len(c.outputs)) || vectorCount != int64(len(c.indexContents)) {
			return errPersonalExactDocument
		}
		ids := make([]string, 0, len(c.outputs))
		for id := range c.outputs {
			ids = append(ids, id)
		}
		// A private projection has no AfterFind hook. The exact owner, file and
		// generation are already locked above; only known output IDs are read.
		type chunkProjection types.Chunk
		var stored []chunkProjection
		if err := tx.Table("chunks").Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"id IN ? AND tenant_id=? AND knowledge_id=? AND knowledge_base_id=?",
			ids, fresh.tenant, fresh.knowledge, fresh.kb,
		).Find(&stored).Error; err != nil {
			return err
		}
		if len(stored) != len(ids) {
			return errPersonalExactDocument
		}
		textCount := 0
		for i := range stored {
			chunk := (*types.Chunk)(&stored[i])
			if chunk.IsEnabled || chunk.DeletedAt.Valid {
				return errPersonalExactDocument
			}
			if err := c.verifyParserChunks([]*types.Chunk{chunk}); err != nil {
				return err
			}
			if chunk.ChunkType == types.ChunkTypeText && chunk.Content != "" {
				textCount++
			}
		}
		if textCount == 0 {
			return errPersonalExactDocument
		}
		var vectors []exactPublishedVector
		if err := tx.Table("embeddings").Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"knowledge_id=? AND knowledge_base_id=? AND source_id IN ?", fresh.knowledge, fresh.kb, ids,
		).Find(&vectors).Error; err != nil {
			return err
		}
		if len(vectors) != len(c.indexContents) {
			return errPersonalExactDocument
		}
		vectorHashes := make(map[string]string, len(vectors))
		c.resultMu.Lock()
		defer c.resultMu.Unlock()
		for _, vector := range vectors {
			content, exists := c.indexContents[vector.SourceID]
			want, proved := c.modelVectors[retriever.EmbeddingInputDigest(ctx, vector.Content)]
			got := exactHalfVectorDigest(vector.Embedding.Slice())
			if !exists || !proved || vector.IsEnabled || vector.Content != content ||
				vector.SourceID != vector.ChunkID || vector.SourceType != int(types.ChunkSourceType) ||
				vector.Dimension <= 0 || vector.Dimension != len(vector.Embedding.Slice()) || got != want {
				return errPersonalExactDocument
			}
			if _, duplicate := vectorHashes[vector.SourceID]; duplicate {
				return errPersonalExactDocument
			}
			vectorHashes[vector.SourceID] = hex.EncodeToString(got[:])
		}
		var knowledge struct {
			Description    string
			Profile        *types.KnowledgeProfile
			CustomMetadata types.JSON
			ParseStatus    string
			EnableStatus   string
		}
		if err := tx.Table("knowledges").Select("description,profile,custom_metadata,parse_status,enable_status").Where(
			"id=? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL",
			fresh.knowledge, fresh.tenant, fresh.kb,
		).Take(&knowledge).Error; err != nil {
			return err
		}
		derived, err := exactDerivedDigest(knowledge.Description, knowledge.Profile)
		if err != nil || derived != c.derived || knowledge.ParseStatus != types.ParseStatusProcessing ||
			knowledge.EnableStatus != "disabled" ||
			(&types.Knowledge{CustomMetadata: knowledge.CustomMetadata}).CustomMetadataText() != "" {
			return errPersonalExactDocument
		}
		chunkJSON, err := json.Marshal(c.outputs)
		if err != nil {
			return err
		}
		vectorJSON, err := json.Marshal(vectorHashes)
		if err != nil {
			return err
		}
		chunksSHA, vectorsSHA := sha256.Sum256(chunkJSON), sha256.Sum256(vectorJSON)
		source.ParserOutputSHA256 = c.parsed.outputSHA256
		source.ChunksSHA256, source.EmbeddingsSHA256 = hex.EncodeToString(
			chunksSHA[:],
		), hex.EncodeToString(
			vectorsSHA[:],
		)
		source.DerivedSHA256 = hex.EncodeToString(derived[:])
		source.TextChunks, source.VectorRows = textCount, len(vectors)
		result := tx.Model(&types.FeishuPersonalResource{}).Where(
			"import_id=? AND current_attempt_id=? AND generation=? AND active_version_id=? AND candidate_version_id=?",
			fresh.importID, fresh.attempt, fresh.generation, fresh.expectedActive, fresh.candidate,
		).Update("active_version_id", fresh.candidate)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return repository.ErrPersonalFence
		}
		for _, table := range []string{"chunks", "embeddings"} {
			if err := tx.Table(table).Where("knowledge_id=? AND knowledge_base_id=?", fresh.knowledge, fresh.kb).
				Update("is_enabled", true).Error; err != nil {
				return err
			}
		}
		if err := tx.Table("knowledges").Where("id=? AND tenant_id=?", fresh.knowledge, fresh.tenant).
			Updates(map[string]any{
				"parse_status": types.ParseStatusCompleted, "enable_status": "enabled",
				"summary_status": types.SummaryStatusCompleted, "processed_at": time.Now(), "error_message": "",
			}).Error; err != nil {
			return err
		}
		// Database-side fingerprints let subsequent read/candidate SQL reject
		// changed bodies before materializing them. Only outputs already matched
		// to this parser/model execution above are enrolled in the receipt.
		if err := tx.Raw(`SELECT encode(sha256(convert_to(
(to_jsonb(k)-'created_at'-'updated_at')::text,'UTF8')),'hex')
FROM knowledges k WHERE k.id=? AND k.tenant_id=?`, fresh.knowledge, fresh.tenant).
			Scan(&source.KnowledgeDBSHA256).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT COALESCE(jsonb_object_agg(c.id,
encode(sha256(convert_to((to_jsonb(c)-'created_at'-'updated_at'-'seq_id')::text,'UTF8')),'hex')),
'{}'::jsonb)::text FROM chunks c WHERE c.knowledge_id=? AND c.knowledge_base_id=? AND c.id IN ?`,
			fresh.knowledge, fresh.kb, ids).
			Scan(&source.ChunksDBManifest).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT COALESCE(jsonb_object_agg(e.source_id,
encode(sha256(convert_to((to_jsonb(e)-'created_at'-'updated_at'-'id')::text,'UTF8')),'hex')),
'{}'::jsonb)::text FROM embeddings e WHERE e.knowledge_id=? AND e.knowledge_base_id=? AND e.source_id IN ?`,
			fresh.knowledge, fresh.kb, ids).
			Scan(&source.VectorsDBManifest).Error; err != nil {
			return err
		}
		// Never enroll a row that arrived after the validated output snapshot.
		// Manifest membership is limited to parser/model-owned IDs above; a
		// concurrent unknown insert remains unreceipted and fails closed.
		var finalChunks, finalVectors int64
		if err := tx.Table("chunks").Where("knowledge_id=?", fresh.knowledge).Count(&finalChunks).Error; err != nil {
			return err
		}
		if err := tx.Table("embeddings").Where("knowledge_id=?", fresh.knowledge).
			Count(&finalVectors).Error; err != nil {
			return err
		}
		if finalChunks != int64(len(c.outputs)) || finalVectors != int64(len(c.indexContents)) {
			return errPersonalExactDocument
		}
		if len(source.KnowledgeDBSHA256) != 64 || source.ChunksDBManifest == "{}" || source.VectorsDBManifest == "{}" {
			return errPersonalExactDocument
		}
		payload, err := json.Marshal(source)
		if err != nil {
			return err
		}
		receiptUpdate := tx.Model(&types.TaskPendingOp{}).Where("id=? AND op='staging'", receipt.ID).
			Updates(map[string]any{"op": "published", "payload": types.JSON(payload)})
		if receiptUpdate.Error != nil {
			return receiptUpdate.Error
		}
		if receiptUpdate.RowsAffected != 1 {
			return repository.ErrPersonalFence
		}
		// The persistent dispatch belongs to the original public Knowledge ID,
		// not the private candidate version. Complete it in this same CAS.
		dispatchUpdate := tx.Model(&types.TaskPendingOp{}).Where(
			"scope=? AND scope_id=? AND tenant_id=? AND op='processing'",
			types.KnowledgeDispatchScope, fresh.knowledge, fresh.tenant,
		).Update("op", "completed")
		if dispatchUpdate.Error != nil {
			return dispatchUpdate.Error
		}
		if dispatchUpdate.RowsAffected != 1 {
			return repository.ErrPersonalFence
		}
		var now time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			return err
		}
		if attempt.LeaseUntil == nil || !attempt.LeaseUntil.After(now) {
			return repository.ErrPersonalFence
		}
		attempt.Status, attempt.OwnerNonce, attempt.LastErrorCategory = "indexed", "", ""
		attempt.LeaseUntil = nil
		return tx.Model(&types.FeishuPersonalImport{}).Where("id=?", fresh.importID).Update("status", "indexed").Error
	})
}
