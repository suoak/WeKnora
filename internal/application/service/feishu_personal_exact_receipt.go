package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

func exactSourceReceipt(proof *personalExactDocument) types.PersonalExactReceipt {
	return types.PersonalExactReceipt{
		Version: 1, TenantID: proof.tenant, KnowledgeBaseID: proof.kb, KnowledgeID: proof.knowledge,
		ImportID: proof.importID, AttemptID: proof.attempt, CandidateVersionID: proof.candidate,
		ExpectedActive: proof.expectedActive, Generation: proof.generation,
		GrantEpoch: proof.grantEpoch, AppConfigVersion: proof.appVersion,
		ConnectionIdentity: proof.connectionIdentity, Revision: proof.revision,
		ResourceID: proof.resource, ResourceHandle: proof.resourceHandle, BindingID: proof.bindingID,
		FileSHA256: proof.fileSHA256, ManifestSHA256: proof.manifestSHA256, FileMD5: proof.fileMD5,
	}
}

func exactReceiptSourceIdentity(receipt types.PersonalExactReceipt) types.PersonalExactReceipt {
	receipt.ParserOutputSHA256, receipt.ChunksSHA256, receipt.EmbeddingsSHA256, receipt.DerivedSHA256 = "", "", "", ""
	receipt.KnowledgeDBSHA256, receipt.ChunksDBManifest, receipt.VectorsDBManifest = "", "", ""
	receipt.TextChunks, receipt.VectorRows = 0, 0
	return receipt
}

// Source-file authority cannot bless pre-existing derived bodies. Before the
// first write the row must be empty of derived output; after it, an exact
// staging receipt must bind the current generation and original source.
func (p *PersonalRecovery) exactExecutionReadable(ctx context.Context, proof *personalExactDocument) error {
	var rows []types.TaskPendingOp
	if err := p.service.db.WithContext(ctx).Where("scope=? AND scope_id=?",
		types.PersonalExactReceiptScope, proof.knowledge).Limit(2).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) != 0 {
		var source types.PersonalExactReceipt
		if len(rows) != 1 || rows[0].TenantID != proof.tenant || rows[0].Op != "staging" ||
			json.Unmarshal(rows[0].Payload, &source) != nil || source != exactSourceReceipt(proof) {
			return errPersonalExactDocument
		}
		return nil
	}
	var empty bool
	err := p.service.db.WithContext(ctx).Raw(`SELECT EXISTS(SELECT 1 FROM knowledges k
WHERE k.id=? AND k.tenant_id=? AND k.knowledge_base_id=? AND k.deleted_at IS NULL
AND COALESCE(k.description,'')='' AND (k.profile IS NULL OR k.profile::text='null')
AND (k.custom_metadata IS NULL OR k.custom_metadata::text IN ('{}','null'))
AND NOT EXISTS(SELECT 1 FROM chunks c WHERE c.knowledge_id=k.id)
AND NOT EXISTS(SELECT 1 FROM embeddings e WHERE e.knowledge_id=k.id))`,
		proof.knowledge, proof.tenant, proof.kb).Scan(&empty).Error
	if err != nil {
		return err
	}
	if !empty {
		return errPersonalExactDocument
	}
	return nil
}

// Persist the staging identity BEFORE releasing any content to the public
// consumer. A missing/failed receipt commit grants no authority to proceed.
// Unknown pre-existing output is preserved for review, never cleaned up here.
func (p *PersonalRecovery) beginExactPersonalReceipt(
	ctx context.Context, proof *personalExactDocument,
) error {
	fresh, err := p.revalidateExactPersonalDocument(ctx, proof)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(exactSourceReceipt(fresh))
	if err != nil {
		return err
	}
	return types.WithExactDocumentWrite(ctx, p.service.db, fresh.tenant, fresh.knowledge, fresh.kb,
		func(tx *gorm.DB) error {
			var safe bool
			err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM knowledges k
WHERE k.id=? AND k.tenant_id=? AND k.knowledge_base_id=? AND k.deleted_at IS NULL
AND k.parse_status IN ('pending','processing') AND COALESCE(k.description,'')=''
AND (k.profile IS NULL OR k.profile::text='null')
AND (k.custom_metadata IS NULL OR k.custom_metadata::text IN ('{}','null'))
AND NOT EXISTS(SELECT 1 FROM chunks c WHERE c.knowledge_id=k.id)
AND NOT EXISTS(SELECT 1 FROM embeddings e WHERE e.knowledge_id=k.id)
AND NOT EXISTS(SELECT 1 FROM task_pending_ops r WHERE r.scope=? AND r.scope_id=k.id))`,
				fresh.knowledge, fresh.tenant, fresh.kb, types.PersonalExactReceiptScope).Scan(&safe).Error
			if err != nil {
				return err
			}
			if !safe {
				return repository.ErrPersonalFence
			}
			if err := tx.Create(&types.TaskPendingOp{
				TenantID: fresh.tenant, TaskType: "knowledge:exact_receipt",
				Scope: types.PersonalExactReceiptScope, ScopeID: fresh.knowledge,
				DedupKey: fmt.Sprintf("exact-%s-%d", fresh.attempt, fresh.generation),
				Op:       "staging", Payload: payload, EnqueuedAt: time.Now(),
			}).Error; err != nil {
				return err
			}
			return tx.Table("knowledges").Where("id=? AND tenant_id=? AND knowledge_base_id=?",
				fresh.knowledge, fresh.tenant, fresh.kb).Update("enable_status", "disabled").Error
		})
}
