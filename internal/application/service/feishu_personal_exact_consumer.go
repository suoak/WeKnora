package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/logger"
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
	resuming        bool
	resumeStorage   int64
	resumeEmpty     bool
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

// Automatic admission is a metadata decision for the supported first-create
// protocol, never a global ID grant. Other formats retain their ordinary path.
// Current authorization, source/file proof and Claim/publication CAS still run.
func (p *PersonalRecovery) exactPersonalRecoveryAllowed(ctx context.Context, knowledgeID string) bool {
	if exactPersonalRecoveryAllows(knowledgeID) {
		return true
	}
	if !exactPersonalRecoveryEnabled() || os.Getenv("FEISHU_PERSONAL_EXACT_RECOVERY_AUTOMATIC") != "1" ||
		knowledgeID == "" || p == nil || p.service == nil || p.service.db == nil {
		return false
	}
	var eligible bool
	err := p.service.db.WithContext(ctx).Raw(`SELECT EXISTS(
SELECT 1 FROM knowledges k
JOIN feishu_personal_resources r ON r.logical_knowledge_id=k.id
JOIN feishu_personal_attempts a ON a.id=r.current_attempt_id AND a.import_id=r.import_id
JOIN feishu_personal_imports m ON m.id=r.import_id AND m.knowledge_id=k.id
WHERE k.id=? AND k.deleted_at IS NULL AND lower(k.file_type)='docx'
AND lower(k.file_name) LIKE '%.docx' AND a.logical_knowledge_id=k.id
AND a.caller_tenant_id=k.tenant_id AND m.kb_id=k.knowledge_base_id
AND m.auth_mode='user_oauth' AND a.auth_mode='user_oauth'
AND a.fence=r.generation AND a.expected_active_version=''
AND (a.candidate_version_id=r.candidate_version_id OR
 (a.status='indexed' AND a.candidate_version_id=r.active_version_id))
AND EXISTS(SELECT 1 FROM task_pending_ops o WHERE o.scope=? AND o.scope_id=k.id
AND o.tenant_id=k.tenant_id AND CAST(o.payload->>'payload_version' AS TEXT)='3'))`,
		knowledgeID, types.KnowledgeDispatchScope).Scan(&eligible).Error
	return err == nil && eligible
}

func (p *PersonalRecovery) processExactFirstCreateConsumer(
	ctx context.Context, payload types.DocumentProcessPayload, owner *types.FeishuPersonalAttempt, operationID int64,
) error {
	if owner == nil || owner.LogicalKnowledgeID != payload.KnowledgeID ||
		!p.exactPersonalRecoveryAllowed(ctx, payload.KnowledgeID) {
		return errPersonalExactDocument
	}
	ks := p.service.knowledge.(*knowledgeService)
	stage := "source_proof"
	executionAttempt := 0
	run := func() error {
		proof, err := p.prepareExactPersonalDocument(ctx, owner)
		if err != nil {
			return err
		}
		stage = "execution_context"
		resuming, err := p.exactResumeQueued(ctx, proof.knowledge)
		if err != nil {
			return err
		}
		if resuming {
			if err := p.repo.ActivateExactResume(ctx, owner, exactSourceReceipt(proof)); err != nil {
				return err
			}
		}
		work, err := p.exactConsumerContext(ctx, proof)
		if err != nil {
			return err
		}
		stage = "staging_receipt"
		consumer, _ := exactPersonalConsumerFrom(work)
		consumer.resuming = resuming
		if resuming {
			consumer.resumeEmpty, err = consumer.resumeHasNoOutput(work)
			if err != nil {
				return err
			}
			if err := p.service.db.WithContext(work).Table("knowledges").
				Select("storage_size").Where("id=?", proof.knowledge).
				Scan(&consumer.resumeStorage).Error; err != nil {
				return err
			}
			_, attempt, err := ks.tracker().OpenAttempt(work, proof.knowledge, payload.LangfuseTraceID)
			if err != nil {
				return err
			}
			payload.Attempt = attempt
		} else {
			if err = p.beginExactPersonalReceipt(work, proof); err != nil {
				return err
			}
		}
		work = withAttempt(work, payload.Attempt)
		executionAttempt = payload.Attempt
		identityHash := func(value string) string {
			digest := sha256.Sum256([]byte(value))
			return hex.EncodeToString(digest[:])
		}
		principal, _ := work.Value(types.PrincipalContextKey).(types.Principal)
		logger.Info(logger.WithFields(work, logger.Fields{
			"knowledge_id": proof.knowledge, "attempt_id": proof.attempt,
			"generation": proof.generation, "operation_id": operationID, "resuming": resuming,
			"tenant_id": proof.tenant, "knowledge_base_id": proof.kb,
			"grant_epoch_sha256": identityHash(proof.grantEpoch),
			"connection_sha256":  identityHash(proof.connectionIdentity),
			"app_config_version": proof.appVersion, "revision": proof.revision,
			"file_sha256": proof.fileSHA256, "manifest_sha256": proof.manifestSHA256,
			"principal_type": principal.Type, "principal_sha256": identityHash(principal.ID),
		}), "Personal exact execution source verified")
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
		ks.beginStage(work, proof.knowledge, types.StagePostProcess,
			types.JSONMap{"operation": "personal_exact_summary"})
		if err = retryExactPersonalSummary(work, func() error {
			_, summaryErr := ks.RegenerateKnowledgeSummary(work, proof.knowledge)
			return summaryErr
		}, waitExactPersonalSummary); err != nil {
			return err
		}
		stage = "atomic_publication"
		if err := consumer.publish(work); err != nil {
			return err
		}
		ks.endStage(work, proof.knowledge, types.StagePostProcess,
			types.JSONMap{"published": true})
		ks.tracker().FinalizeAttempt(work, proof.knowledge, payload.Attempt,
			types.SpanStatusDone, types.JSONMap{"receipt_published": true}, "", "")
		return nil
	}
	if err := run(); err != nil {
		logger.WarnWithFields(ctx, exactPersonalPreflightFields(owner, stage, nil, err),
			"Personal exact execution stopped; original outputs retained")
		// An interrupted/ambiguous execution is preserved for review. Never
		// clean up unknown chunks, replay the outbox, or reuse this claim.
		finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		finishErr := p.repo.AbortExactExecution(finish, owner, operationID)
		if executionAttempt > 0 {
			ks.tracker().FinalizeAttempt(finish, owner.LogicalKnowledgeID, executionAttempt,
				types.SpanStatusFailed, nil, "personal_exact_publication_failed",
				"Personal document processing stopped; "+
					"original outputs retained for controlled recovery.")
		}
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
		ParsedSegments:  parsed.segments,
		Metadata: map[string]string{
			"parser": "anydoc", "anydoc_version": parsed.parserVersion, "source_format": "docx",
		},
	}
	for _, asset := range parsed.assets {
		result.ImageRefs = append(result.ImageRefs, types.ImageRef{
			Filename: asset.Name, OriginalRef: anydoc.ImageDir + asset.Name,
			MimeType: asset.MediaType, ImageData: append([]byte(nil), asset.Data...),
			IsOriginal: strings.HasPrefix(asset.Name, "embedded-workbook-"),
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
		imported.KnowledgeID != attempt.LogicalKnowledgeID ||
		p.exactPersonalRecoveryAllowed(ctx, imported.KnowledgeID) {
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
