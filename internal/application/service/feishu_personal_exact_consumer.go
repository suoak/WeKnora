package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

type exactPersonalConsumerKey struct{}

type exactPersonalConsumer struct {
	recovery        *PersonalRecovery
	source          *personalExactDocument
	parsed          *personalExactParsedDocument
	outputs         map[string][32]byte
	embeddingInputs map[[32]byte]bool
	indexed         bool
	derived         [32]byte
	derivedReady    bool
	resultMu        sync.Mutex
	indexContents   map[string]string
	modelVectors    map[[32]byte][32]byte
}

func exactPersonalConsumerFrom(ctx context.Context) (*exactPersonalConsumer, bool) {
	consumer, ok := ctx.Value(exactPersonalConsumerKey{}).(*exactPersonalConsumer)
	return consumer, ok && consumer != nil && consumer.recovery != nil && consumer.source != nil
}

// Explicit server deployment interlock, never a payload/header/metadata flag.
// Production must remain disabled until public-dispatch and serving acceptance
// are complete and the file-recovery deployment receives its own approval.
func exactPersonalRecoveryEnabled() bool {
	return os.Getenv("FEISHU_PERSONAL_EXACT_RECOVERY") == "1"
}

// A production pilot is bounded to explicitly approved original IDs. This is
// an execution interlock, not a source/read authorization or a payload flag.
func exactPersonalRecoveryAllows(knowledgeID string) bool {
	if !exactPersonalRecoveryEnabled() || knowledgeID == "" {
		return false
	}
	for _, approved := range strings.Split(os.Getenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS"), ",") {
		if strings.TrimSpace(approved) == knowledgeID {
			return true
		}
	}
	return false
}

func (p *PersonalRecovery) processExactFirstCreateConsumer(
	ctx context.Context, payload types.DocumentProcessPayload, owner *types.FeishuPersonalAttempt, operationID int64,
) error {
	if !exactPersonalRecoveryAllows(payload.KnowledgeID) {
		return errPersonalExactDocument
	}
	ks := p.service.knowledge.(*knowledgeService)
	stage := "source_proof"
	run := func() error {
		proof, err := p.prepareExactPersonalDocument(ctx, owner)
		if err != nil {
			return err
		}
		stage = "execution_context"
		work, err := p.exactConsumerContext(ctx, proof)
		if err != nil {
			return err
		}
		stage = "staging_receipt"
		if err = p.beginExactPersonalReceipt(work, proof); err != nil {
			return err
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		stage = "public_parser"
		if err = ks.processDocument(work, asynq.NewTask(types.TypeDocumentProcess, body)); err != nil {
			return err
		}
		stage = "index_completion"
		consumer, ok := exactPersonalConsumerFrom(work)
		if !ok || !consumer.indexed {
			return errPersonalExactDocument
		}
		stage = "summary_tenant"
		work, err = restoreSummaryRefreshTenantInfo(work, ks.tenantRepo, proof.tenant)
		if err != nil {
			return err
		}
		stage = "derived_summary"
		if _, err = ks.RegenerateKnowledgeSummary(work, proof.knowledge); err != nil {
			return err
		}
		stage = "atomic_publication"
		return consumer.publish(work)
	}
	if err := run(); err != nil {
		// An interrupted/ambiguous execution is preserved for review. Never
		// clean up unknown chunks, replay the outbox, or reuse this claim.
		finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		finishErr := p.repo.AbortExactExecution(finish, owner, operationID)
		return errors.Join(fmt.Errorf("personal_exact_stage_%s: %w", stage, err), finishErr, asynq.SkipRetry)
	}
	return nil
}

func (p *PersonalRecovery) exactConsumerContext(
	ctx context.Context, proof *personalExactDocument,
) (context.Context, error) {
	ctx, err := p.exactDocumentExecutionContext(ctx, proof)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, exactPersonalConsumerKey{}, &exactPersonalConsumer{recovery: p, source: proof}), nil
}

func (c *exactPersonalConsumer) convert(
	ctx context.Context, payload types.DocumentProcessPayload,
) (*types.ReadResult, error) {
	proof := c.source
	if payload.KnowledgeID != proof.knowledge || payload.KnowledgeBaseID != proof.kb ||
		payload.TenantID != proof.tenant || payload.PersonalAttemptID != proof.attempt ||
		payload.CandidateVersionID != proof.candidate || !payload.FirstCreate ||
		payload.URL != "" || payload.FileURL != "" || len(payload.Passages) != 0 ||
		strings.TrimPrefix(strings.ToLower(payload.FileType), ".") != "docx" {
		return nil, fmt.Errorf("exact_document_task_binding_unverified: %w", errPersonalExactDocument)
	}
	parsed, err := c.recovery.parseExactPersonalDocument(ctx, proof)
	if err != nil {
		return nil, err
	}
	c.parsed = parsed
	// This is the same AnyDoc result consumed by the normal image resolver and
	// chunker. Paths are logical references to checked embedded bytes; no URL or
	// task-supplied file is reopened between verification and parsing.
	result := &types.ReadResult{
		MarkdownContent: parsed.markdown,
		Metadata: map[string]string{
			"parser": "anydoc", "anydoc_version": parsed.parserVersion, "source_format": "docx",
		},
	}
	for _, asset := range parsed.assets {
		result.ImageRefs = append(result.ImageRefs, types.ImageRef{
			Filename: asset.Name, OriginalRef: anydoc.ImageDir + asset.Name,
			MimeType: asset.MediaType, ImageData: append([]byte(nil), asset.Data...),
		})
	}
	return result, nil
}

// Configuration is read as a projection, never as a KB entity whose profile or
// description could contain restricted historical source data. Canonical KB
// ownership remains mandatory and no user-facing KB read is granted here.
func (s *knowledgeService) processingKnowledgeBase(
	ctx context.Context, kbID string,
) (*types.KnowledgeBase, error) {
	consumer, exact := exactPersonalConsumerFrom(ctx)
	if !exact {
		return s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	}
	proof := consumer.source
	if proof.kb != kbID {
		return nil, repository.ErrPersonalIdentity
	}
	if _, err := consumer.recovery.revalidateExactPersonalDocument(ctx, proof); err != nil {
		return nil, err
	}
	owner, err := personalAuthorizationKB(consumer.recovery.service.db.WithContext(ctx), kbID, false)
	if err != nil || owner.TenantID != proof.tenant {
		return nil, repository.ErrPersonalIdentity
	}
	kb := &types.KnowledgeBase{}
	// Scan deliberately avoids KnowledgeBase.AfterFind. No source body or
	// generated profile is selected. Storage/model IDs are configuration only.
	err = consumer.recovery.service.db.WithContext(ctx).Table("knowledge_bases").Select(`
id,tenant_id,creator_id,type,chunking_config,image_processing_config,
embedding_model_id,summary_model_id,vlm_config,asr_config,storage_provider_config,
storage_backend_id,cos_config,vector_store_id,extract_config,faq_config,
question_generation_config,auto_tag_config,profile_config,indexing_strategy`).Where(
		"id=? AND tenant_id=? AND deleted_at IS NULL", kbID, proof.tenant,
	).Scan(kb).Error
	if err != nil {
		return nil, err
	}
	if kb.ID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	kb.PrivateOwnerID = owner.PrivateOwnerID
	return kb, nil
}

// Unclaimed and already-published protocols are preserved on authorization
// failure. Only the matching claimed owner can close a processing ledger.
func (p *PersonalRecovery) closeExactReconcileExecution(
	ctx context.Context, owner *types.FeishuPersonalAttempt,
) error {
	if owner == nil || owner.Status != "processing" || owner.OwnerNonce == "" {
		return nil
	}
	var op types.TaskPendingOp
	if err := p.service.db.WithContext(ctx).Where(
		"tenant_id=? AND scope=? AND scope_id=?", owner.CallerTenantID,
		types.KnowledgeDispatchScope, owner.LogicalKnowledgeID,
	).Take(&op).Error; err != nil {
		return err
	}
	return p.repo.AbortExactExecution(ctx, owner, op.ID)
}

// A single-file recovery window must not turn an unrelated held first-create
// protocol into an authorization failure or a new execution. This is metadata
// admission only; healthy ordinary imports keep their existing lifecycle.
func (p *PersonalRecovery) exactRecoveryDeferred(ctx context.Context,
	imported *types.FeishuPersonalImport, attempt *types.FeishuPersonalAttempt,
) (bool, error) {
	if !p.firstCreateOnly || !exactPersonalRecoveryEnabled() || imported == nil || attempt == nil ||
		imported.AuthMode != types.FeishuUserOAuth || imported.KnowledgeID == "" ||
		imported.KnowledgeID != attempt.LogicalKnowledgeID || exactPersonalRecoveryAllows(imported.KnowledgeID) {
		return false, nil
	}
	var held bool
	err := p.service.db.WithContext(ctx).Raw(`SELECT EXISTS(
SELECT 1 FROM task_pending_ops op WHERE op.tenant_id=? AND op.scope=? AND op.scope_id=?
AND op.payload->>'payload_version'='3') AND (EXISTS(
SELECT 1 FROM data_sources d WHERE d.knowledge_base_id=? AND d.legacy_access_state IN ('hold','revoked'))
OR EXISTS(SELECT 1 FROM feishu_legacy_resources r
WHERE r.knowledge_base_id=? AND r.state IN ('hold','revoked')))`,
		attempt.CallerTenantID, types.KnowledgeDispatchScope, imported.KnowledgeID,
		imported.KBID, imported.KBID).Scan(&held).Error
	return held, err
}
